package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrUnknownService = errors.New("service not found in compose file")

type servicesEntry struct {
	key      string
	services []string
}

type servicesCache struct {
	mu       sync.Mutex
	entries  map[string]servicesEntry
	inflight map[string]*servicesCall
}

// servicesCall is one in-flight `compose config --services`; concurrent snapshots wait for it
// instead of each forking their own compose process for the same file.
type servicesCall struct {
	done     chan struct{}
	key      string
	services []string
	err      error
}

func cacheKey(file string) string {
	key := ""
	for _, p := range []string{file, filepath.Join(filepath.Dir(file), ".env")} {
		if st, err := os.Stat(p); err == nil { //nolint:gosec // p is a discovered compose file or its .env
			key += fmt.Sprintf("%d:%d;", st.ModTime().UnixNano(), st.Size())
		}
	}
	return key
}

func (s *Service) composeServices(ctx context.Context, file string) ([]string, error) {
	key := cacheKey(file) // file is always one of the discovered compose files
	c := s.svcCache
	c.mu.Lock()
	if e, ok := c.entries[file]; ok && e.key == key {
		c.mu.Unlock()
		return e.services, nil
	}
	if call, ok := c.inflight[file]; ok && call.key == key {
		c.mu.Unlock()
		select {
		case <-call.done:
			return call.services, call.err
		case <-ctx.Done():
			return nil, fmt.Errorf("compose config --services: %w", ctx.Err())
		}
	}
	call := &servicesCall{done: make(chan struct{}), key: key}
	if c.inflight == nil {
		c.inflight = map[string]*servicesCall{}
	}
	c.inflight[file] = call
	c.mu.Unlock()

	// Detached from the caller: waiters with longer deadlines share this result.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), servicesTimeout)
	defer cancel()
	call.services, call.err = s.listServices(runCtx, file)

	c.mu.Lock()
	if call.err == nil {
		c.entries[file] = servicesEntry{key: key, services: call.services}
	}
	if c.inflight[file] == call {
		delete(c.inflight, file)
	}
	c.mu.Unlock()
	close(call.done)
	return call.services, call.err
}

const servicesTimeout = 30 * time.Second

func (s *Service) listServices(ctx context.Context, file string) ([]string, error) {
	out, err := s.docker.Compose(ctx, file, "config", "--services")
	if err != nil {
		return nil, fmt.Errorf("compose config --services: %w", err)
	}
	var services []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.Contains(line, " ") {
			services = append(services, line)
		}
	}
	sort.Strings(services)
	return services, nil
}

func (s *Service) withServices(ctx context.Context, files []ComposeFile) []ComposeFile {
	var wg sync.WaitGroup
	for i := range files {
		wg.Add(1)
		go func(f *ComposeFile) {
			defer wg.Done()
			services, err := s.composeServices(ctx, f.Path)
			if err != nil {
				f.Error = err.Error()
				s.log.Warn("compose services failed", slog.String("file", f.Path), slog.Any("error", err))
			}
			if services == nil {
				services = []string{}
			}
			f.Services = services
			disabled, err := s.disabled.For(f.Path)
			if err != nil {
				f.Error = err.Error()
			}
			if disabled == nil {
				disabled = []string{}
			}
			f.Disabled = disabled
		}(&files[i])
	}
	wg.Wait()
	return files
}

type ToggleRequest struct {
	ComposeFile string `json:"composeFile"`
	Service     string `json:"service"`
	Disabled    bool   `json:"disabled"`
}

type ToggleResult struct {
	ComposeFile string   `json:"composeFile"`
	Service     string   `json:"service"`
	Disabled    bool     `json:"disabled"`
	Dependents  []string `json:"dependents"`
	StorePath   string   `json:"storePath"`
}

func (s *Service) dependents(ctx context.Context, file, service string, disabled []string) []string {
	raw, err := s.configJSON(ctx, file)
	if err != nil {
		return nil
	}
	var cfg struct {
		Services map[string]struct {
			DependsOn map[string]json.RawMessage `json:"depends_on"` //nolint:tagliatelle // docker compose config schema
		} `json:"services"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	var out []string
	for name, svc := range cfg.Services {
		if _, ok := svc.DependsOn[service]; ok && !slices.Contains(disabled, name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) SetDisabled(ctx context.Context, req ToggleRequest) (ToggleResult, error) {
	files, _, err := s.composeFiles()
	if err != nil {
		return ToggleResult{}, err
	}
	var file *ComposeFile
	for i := range files {
		if files[i].Path == req.ComposeFile {
			file = &files[i]
		}
	}
	if file == nil {
		return ToggleResult{}, fmt.Errorf("%w: %s", ErrUnknownService, req.ComposeFile)
	}
	services, err := s.composeServices(ctx, file.Path)
	if err != nil {
		return ToggleResult{}, err
	}
	if !slices.Contains(services, req.Service) {
		return ToggleResult{}, fmt.Errorf("%w: %s", ErrUnknownService, req.Service)
	}
	if err := s.disabled.Set(file.Path, req.Service, req.Disabled); err != nil {
		return ToggleResult{}, fmt.Errorf("save disabled: %w", err)
	}
	s.log.Info("service autostart toggled", slog.String("file", file.Rel), slog.String("service", req.Service), slog.Bool("disabled", req.Disabled))

	res := ToggleResult{ComposeFile: file.Path, Service: req.Service, Disabled: req.Disabled, Dependents: []string{}, StorePath: s.disabled.Path()}
	if req.Disabled {
		disabled, _ := s.disabled.For(file.Path)
		if deps := s.dependents(ctx, file.Path, req.Service, disabled); deps != nil {
			res.Dependents = deps
		}
	}
	return res, nil
}
