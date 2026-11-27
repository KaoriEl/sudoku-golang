package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sudoku-golang/internal/backup"
	"sudoku-golang/internal/docker"
	"time"
)

const (
	runBackup  = "backup"
	runRestore = "restore"
	maxBackups = 50
)

type BackupTarget struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Project     string     `json:"project"`
	Service     string     `json:"service"`
	ComposeFile string     `json:"composeFile"`
	Image       string     `json:"image"`
	Kind        string     `json:"kind"`
	Running     bool       `json:"running"`
	Last        *time.Time `json:"last"`
	Count       int        `json:"count"`
}

type BackupsView struct {
	Dir     string          `json:"dir"`
	Keep    int             `json:"keep"`
	Targets []BackupTarget  `json:"targets"`
	Backups []backup.Backup `json:"backups"`
}

func (s *Service) Backups(ctx context.Context) (BackupsView, error) {
	p, _ := s.prefs.Get()
	p = p.Normalized()
	list, err := s.backups.List()
	if err != nil {
		return BackupsView{}, err //nolint:wrapcheck // descriptive
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return BackupsView{}, err
	}
	view := BackupsView{Dir: s.backups.Dir(), Keep: p.Backups.Keep, Targets: []BackupTarget{}, Backups: list}
	for _, c := range snap.Containers {
		kind, ok := backup.Detect(c.Image)
		if !ok || !c.Managed {
			continue
		}
		t := BackupTarget{
			ID: c.ID, Name: c.Name, Project: c.Project, Service: c.Service, ComposeFile: c.ComposeFile,
			Image: c.Image, Kind: string(kind), Running: c.State == dockerRunning,
		}
		for _, b := range list {
			if b.Container == c.Name {
				t.Count++
				if t.Last == nil {
					at := b.CreatedAt
					t.Last = &at
				}
			}
		}
		view.Targets = append(view.Targets, t)
	}
	return view, nil
}

func (s *Service) fileOf(path string) ComposeFile {
	files, _, _ := s.composeFiles()
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	return ComposeFile{Path: path, Rel: filepath.Base(path)}
}

func (s *Service) inspectManaged(ctx context.Context, id string) (Container, docker.Container, error) {
	c, err := s.FindContainer(ctx, id)
	if err != nil {
		return Container{}, docker.Container{}, err
	}
	if !c.Managed {
		return Container{}, docker.Container{}, ErrNotManaged
	}
	raw, err := s.source.Inspect(ctx, c.ID)
	if err != nil {
		return Container{}, docker.Container{}, err //nolint:wrapcheck // descriptive
	}
	return c, raw, nil
}

// CreateBackups dumps the given database containers one after another as a run.
func (s *Service) CreateBackups(ctx context.Context, ids []string) (*Run, error) {
	if len(ids) == 0 || len(ids) > maxBackups {
		return nil, &userError{msg: "выберите от 1 до " + strconv.Itoa(maxBackups) + " баз"}
	}
	p, _ := s.prefs.Get()
	keep := p.Normalized().Backups.Keep
	plan := runPlan{action: runBackup, target: strconv.Itoa(len(ids))}
	for _, id := range ids {
		c, raw, err := s.inspectManaged(ctx, id)
		if err != nil {
			return nil, err
		}
		if _, ok := backup.Detect(c.Image); !ok {
			return nil, &userError{msg: fmt.Sprintf("%s: %s", c.Name, backup.ErrUnsupported)}
		}
		plan.steps = append(plan.steps, stepPlan{
			file:    s.fileOf(c.ComposeFile),
			phase:   runBackup,
			display: "backup " + c.Name,
			fn: func(ctx context.Context, out io.Writer) error {
				if _, err := s.backups.Create(ctx, raw, out); err != nil {
					return err //nolint:wrapcheck // descriptive
				}
				s.backups.Prune(c.Name, keep)
				return nil
			},
		})
	}
	return s.startRun(plan, false)
}

// RestoreBackup loads a dump into the container it was taken from (by name), or into containerID.
func (s *Service) RestoreBackup(ctx context.Context, id, containerID string) (*Run, error) {
	b, err := s.backups.Get(id)
	if err != nil {
		return nil, &userError{msg: err.Error()}
	}
	if containerID == "" {
		containerID = b.Container
	}
	c, raw, err := s.inspectManaged(ctx, containerID)
	if err != nil {
		return nil, err
	}
	plan := runPlan{action: runRestore, target: c.Name, steps: []stepPlan{{
		file:    s.fileOf(c.ComposeFile),
		phase:   runRestore,
		display: "restore " + b.ID + " → " + c.Name,
		fn: func(ctx context.Context, out io.Writer) error {
			s.alerts.expect(raw.ID) // a redis restore stops and starts the server on purpose
			return s.backups.Restore(ctx, b, raw, out)
		},
	}}}
	return s.startRun(plan, true)
}

func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	v, err := s.svc.Backups(ctx)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleCreateBackups(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Containers []string `json:"containers"`
	}
	if err := decodeBody(r, &body); err != nil {
		s.writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	run, err := s.svc.CreateBackups(ctx, body.Containers)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID        string `json:"id"`
		Container string `json:"container"`
	}
	if err := decodeBody(r, &body); err != nil {
		s.writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	run, err := s.svc.RestoreBackup(ctx, body.ID, body.Container)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeBody(r, &body); err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.svc.backups.Delete(body.ID); err != nil {
		s.writeError(w, &userError{msg: err.Error()})
		return
	}
	s.log.Info("backup deleted", "id", body.ID)
	writeJSON(w, http.StatusOK, map[string]string{"id": body.ID})
}

// handleDownloadBackup serves the dump file (ServeContent: Range requests, no buffering).
func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	b, err := s.svc.backups.Get(r.URL.Query().Get("id"))
	if errors.Is(err, backup.ErrBadID) || err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "backup not found", Code: codeNotFound})
		return
	}
	f, err := os.Open(b.File)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "backup not found", Code: codeNotFound})
		return
	}
	defer func() { _ = f.Close() }()
	name := unsafeFileChars.ReplaceAllString(b.Container, "_") + "-" + filepath.Base(b.File)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, name, b.CreatedAt, f)
}
