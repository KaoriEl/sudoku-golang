package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// The infra editor edits the two files that make up the infrastructure: the infra compose.yaml and
// Traefik's static config. Writes are guarded by the hash the editor loaded (no silent overwrite of
// changes made meanwhile by hand, by the ports page or by another tab), validated (YAML, and for
// compose a real `compose config` run on a temp copy next to the file so relative paths resolve the
// same way) and go through writeFileAtomic, which keeps a .sudoku.bak copy.

const (
	infraFileCompose = "compose"
	infraFileTraefik = "traefik"
	runInfraApply    = "infra-apply"
	validateTimeout  = 30 * time.Second
	maxCheckOutput   = 2000
)

var (
	ErrUnknownInfraFile = errors.New("unknown infra file")
	ErrStaleFile        = errors.New("файл изменился на диске после открытия — обновите редактор, чтобы не затереть чужие правки")
)

type InfraFile struct {
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	Exists  bool      `json:"exists"`
	Content string    `json:"content"`
	Hash    string    `json:"hash"`
	ModTime time.Time `json:"modTime"`
	HasBak  bool      `json:"hasBackup"`
}

type InfraFilesView struct {
	Files []InfraFile `json:"files"`
}

type InfraSaveRequest struct {
	Content string `json:"content"`
	Hash    string `json:"hash"`
	Apply   bool   `json:"apply"`
}

type InfraSaved struct {
	File InfraFile `json:"file"`
	Run  *Run      `json:"run,omitempty"`
	// ApplyError: the file is saved but the apply run could not start (e.g. another run is active).
	ApplyError string `json:"applyError,omitempty"`
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Service) infraPath(id string) (string, error) {
	switch id {
	case infraFileCompose:
		return s.layout.InfraFile(), nil
	case infraFileTraefik:
		if inf, err := s.infra(); err == nil {
			return inf.traefikCfg, nil
		}
		return filepath.Join(s.layout.ConfigDir(), "traefik", "traefik.yaml"), nil
	}
	return "", fmt.Errorf("%w: %s", ErrUnknownInfraFile, id)
}

func (s *Service) readInfraFile(id string) (InfraFile, error) {
	p, err := s.infraPath(id)
	if err != nil {
		return InfraFile{}, err
	}
	f := InfraFile{ID: id, Path: p, Hash: hashOf(nil)}
	raw, err := os.ReadFile(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return f, nil
	case err != nil:
		return f, fmt.Errorf("read %s: %w", p, err)
	}
	f.Exists, f.Content, f.Hash = true, string(raw), hashOf(raw)
	if st, err := os.Stat(p); err == nil {
		f.ModTime = st.ModTime().UTC()
	}
	_, bakErr := os.Stat(p + ".sudoku.bak")
	f.HasBak = bakErr == nil
	return f, nil
}

func (s *Service) InfraFiles() (InfraFilesView, error) {
	view := InfraFilesView{Files: []InfraFile{}}
	for _, id := range []string{infraFileCompose, infraFileTraefik} {
		f, err := s.readInfraFile(id)
		if err != nil {
			return view, err
		}
		view.Files = append(view.Files, f)
	}
	return view, nil
}

func checkYAML(content string) error {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return &userError{msg: "YAML не разбирается: " + err.Error()}
	}
	if len(doc) == 0 {
		return &userError{msg: "файл пустой"}
	}
	return nil
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxCheckOutput {
		return s[:maxCheckOutput] + "…"
	}
	return s
}

// validateCompose runs `compose config --quiet` on a temp copy in the same directory, so relative
// volumes, env_file and the folder's .env behave exactly like for the real file.
func (s *Service) validateCompose(ctx context.Context, path, content string) error {
	var doc struct {
		Services map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil || len(doc.Services) == 0 {
		return &userError{msg: "в compose.yaml нет секции services"}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sudoku-check-*.yaml")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, validateTimeout)
	defer cancel()
	if _, stderr, err := s.docker.ComposeOutput(ctx, tmp.Name(), "config", "--quiet"); err != nil {
		msg := clip(strings.ReplaceAll(stderr, tmp.Name(), filepath.Base(path)))
		if msg == "" {
			msg = err.Error()
		}
		return &userError{msg: "docker compose отклонил файл: " + msg}
	}
	return nil
}

// SaveInfraFile validates and writes one infra file; with Apply it starts a run that brings the change
// up (compose: `up -d` of the enabled services; traefik: recreate the Traefik container).
func (s *Service) SaveInfraFile(ctx context.Context, id string, req InfraSaveRequest) (InfraSaved, error) {
	p, err := s.infraPath(id)
	if err != nil {
		return InfraSaved{}, err
	}
	if err := checkYAML(req.Content); err != nil {
		return InfraSaved{}, err
	}
	if id == infraFileCompose {
		if err := s.validateCompose(ctx, p, req.Content); err != nil {
			return InfraSaved{}, err
		}
	}

	s.files.Lock()
	current, err := s.readInfraFile(id)
	if err == nil && current.Hash != req.Hash {
		err = ErrStaleFile
	}
	if err == nil && current.Content != req.Content {
		if err = os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
			err = writeFileAtomic(p, []byte(req.Content))
		}
	}
	s.files.Unlock()
	if err != nil {
		return InfraSaved{}, err
	}
	s.log.Info("infra file saved", "file", p)
	s.hub.markState()

	saved := InfraSaved{}
	if saved.File, err = s.readInfraFile(id); err != nil {
		return saved, err
	}
	if req.Apply {
		if saved.Run, err = s.applyInfra(ctx, id); err != nil {
			saved.ApplyError = err.Error()
		}
	}
	return saved, nil
}

func (s *Service) applyInfra(ctx context.Context, id string) (*Run, error) {
	if id == infraFileTraefik {
		return s.ApplyPorts()
	}
	files, _, err := s.composeFiles()
	if err != nil {
		return nil, err
	}
	var infra []ComposeFile
	for _, f := range files {
		if f.Infra {
			infra = append(infra, f)
		}
	}
	if len(infra) == 0 {
		return nil, ErrNoInfraCompose
	}
	plan, _ := s.bulkPlan(actionStart, s.withServices(ctx, infra))
	plan.action, plan.target = runInfraApply, "infra"
	return s.startRun(plan, true)
}
