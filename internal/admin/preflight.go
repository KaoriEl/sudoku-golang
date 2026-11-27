package admin

import (
	"context"
	"net/http"
	"sudoku-golang/internal/portcheck"
	"sync"
)

type configEntry struct {
	key  string
	json []byte
}

// configCache keeps `compose config --format json` per file until the file or its .env changes.
type configCache struct {
	mu      sync.Mutex
	entries map[string]configEntry
}

func (s *Service) configJSON(ctx context.Context, file string) ([]byte, error) {
	key := cacheKey(file)
	s.cfgCache.mu.Lock()
	if e, ok := s.cfgCache.entries[file]; ok && e.key == key {
		s.cfgCache.mu.Unlock()
		return e.json, nil
	}
	s.cfgCache.mu.Unlock()
	raw, err := s.docker.ComposeConfigJSON(ctx, file)
	if err != nil {
		return nil, err //nolint:wrapcheck // CommandError is descriptive
	}
	s.cfgCache.mu.Lock()
	if s.cfgCache.entries == nil {
		s.cfgCache.entries = map[string]configEntry{}
	}
	s.cfgCache.entries[file] = configEntry{key: key, json: raw}
	s.cfgCache.mu.Unlock()
	return raw, nil
}

type PreflightRequest struct {
	Targets []SelectionTarget `json:"targets"`
	Set     string            `json:"set"`
}

type PreflightResult struct {
	Conflicts []portcheck.Conflict `json:"conflicts"`
	Checked   int                  `json:"checked"`
}

// Preflight reports host ports that the services about to start cannot bind. No targets and no set
// means every compose file (bulk start).
func (s *Service) Preflight(ctx context.Context, req PreflightRequest) (PreflightResult, error) {
	groups := map[string][]string{}
	var order []string
	switch {
	case req.Set != "":
		set, err := s.sets.Get(req.Set)
		if err != nil {
			return PreflightResult{}, &userError{msg: err.Error()}
		}
		_, projects := s.projects(ctx)
		entries, err := resolveEntries(projects, set.Targets)
		if err != nil {
			return PreflightResult{}, err
		}
		for _, e := range entries {
			order = append(order, e.file)
			groups[e.file] = e.services
		}
	case len(req.Targets) > 0:
		files, g, err := s.selectionGroups(ctx, req.Targets)
		if err != nil {
			return PreflightResult{}, err
		}
		for _, f := range files {
			order = append(order, f.Path)
		}
		groups = g
	default:
		files, _, err := s.composeFiles()
		if err != nil {
			return PreflightResult{}, err
		}
		for _, f := range files {
			order = append(order, f.Path)
		}
	}
	running, err := s.source.List(ctx)
	if err != nil {
		return PreflightResult{}, err //nolint:wrapcheck // descriptive
	}
	res := PreflightResult{Conflicts: []portcheck.Conflict{}}
	for _, file := range order {
		raw, err := s.configJSON(ctx, file)
		if err != nil {
			continue
		}
		bindings, err := portcheck.Published(raw, file, groups[file])
		if err != nil {
			continue
		}
		res.Checked += len(bindings)
		res.Conflicts = append(res.Conflicts, portcheck.Check(ctx, bindings, running)...)
	}
	return res, nil
}

func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	var req PreflightRequest
	if err := decodeBody(r, &req); err != nil {
		s.writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	res, err := s.svc.Preflight(ctx, req)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
