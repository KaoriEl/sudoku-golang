package admin

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sudoku-golang/internal/service"
)

type SetView struct {
	service.Set
	Resolved []SelectionTarget `json:"resolved"`
	Files    []string          `json:"files"`
	Error    string            `json:"error"`
}

type SetsView struct {
	Path string    `json:"path"`
	Sets []SetView `json:"sets"`
}

// projects returns the discovered compose files as targets see them, with their services.
func (s *Service) projects(ctx context.Context) ([]ComposeFile, []service.Project) {
	files, _, err := s.composeFiles()
	if err != nil {
		return nil, nil
	}
	files = s.withServices(ctx, files)
	root, _ := filepath.Abs(s.cfg.RootProjectsFolder)
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	projects := service.ProjectsFrom(root, paths)
	for i := range projects {
		projects[i].Services = files[i].Services
	}
	return files, projects
}

func (s *Service) Sets(ctx context.Context) (SetsView, error) {
	sets, err := s.sets.List()
	if err != nil {
		return SetsView{}, err //nolint:wrapcheck // descriptive
	}
	_, projects := s.projects(ctx)
	view := SetsView{Path: s.sets.Path(), Sets: make([]SetView, 0, len(sets))}
	for _, set := range sets {
		v := SetView{Set: set, Resolved: []SelectionTarget{}, Files: []string{}}
		entries, err := service.Resolve(projects, set.Targets)
		if err != nil {
			v.Error = err.Error()
		}
		for _, e := range entries {
			v.Files = append(v.Files, e.Project.Rel)
			services := e.Services
			if services == nil {
				services = e.Project.Services
			}
			for _, svc := range services {
				v.Resolved = append(v.Resolved, SelectionTarget{ComposeFile: e.Project.File, Service: svc})
			}
		}
		view.Sets = append(view.Sets, v)
	}
	return view, nil
}

func (s *Service) SaveSet(ctx context.Context, set service.Set) (SetsView, error) {
	_, projects := s.projects(ctx)
	if _, err := service.Resolve(projects, set.Targets); err != nil {
		return SetsView{}, &userError{msg: err.Error()}
	}
	if _, err := s.sets.Upsert(set); err != nil {
		if errors.Is(err, service.ErrBadSetName) || errors.Is(err, service.ErrEmptySet) {
			return SetsView{}, &userError{msg: err.Error()}
		}
		return SetsView{}, err //nolint:wrapcheck // descriptive
	}
	s.log.Info("set saved", "name", set.Name, "targets", strings.Join(set.Targets, " "))
	return s.Sets(ctx)
}

// RunSet starts/stops/restarts a set as one run. Whole projects keep the "don't start" flags of
// their services; services named explicitly in the set are started even if flagged.
func (s *Service) RunSet(ctx context.Context, name, action string) (*Run, error) {
	set, err := s.sets.Get(name)
	if err != nil {
		return nil, &userError{msg: err.Error()}
	}
	files, projects := s.projects(ctx)
	entries, err := service.Resolve(projects, set.Targets)
	if err != nil {
		return nil, &userError{msg: err.Error()}
	}
	byPath := map[string]ComposeFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	plan := runPlan{action: "set-" + action, target: set.Name}
	var stops, ups []stepPlan
	for _, e := range entries {
		f := byPath[e.Project.File]
		stopArgs := append([]string{actionStop}, e.Services...)
		stops = append(stops, stepPlan{file: f, phase: actionStop, args: stopArgs, parallel: true})
		up := upStep(f, e.Services)
		if e.Services == nil && len(f.Disabled) > 0 {
			enabled := service.EnabledServices(f.Services, f.Disabled)
			up.args = append(up.args, enabled...)
			if len(enabled) == 0 {
				up.skipReason = "все сервисы отключены: " + strings.Join(f.Disabled, ", ")
			}
		}
		ups = append(ups, up)
	}
	switch action {
	case actionStart:
		plan.steps = ups
	case actionStop:
		plan.steps = stops
	case actionRestart:
		plan.steps = append(append(make([]stepPlan, 0, len(stops)+len(ups)), stops...), ups...)
	default:
		return nil, ErrUnknownAction
	}
	plan.steps = s.withHealthWait(plan.steps)
	return s.startRun(plan, false)
}

func (s *Server) handleSets(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	v, err := s.svc.Sets(ctx)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleSaveSet(w http.ResponseWriter, r *http.Request) {
	var body service.Set
	var raw struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Targets     []string `json:"targets"`
	}
	if err := decodeBody(r, &raw); err != nil {
		s.writeError(w, err)
		return
	}
	body = service.Set{Name: raw.Name, Description: raw.Description, Targets: raw.Targets}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	v, err := s.svc.SaveSet(ctx, body)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeBody(r, &body); err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.svc.sets.Delete(body.Name); err != nil {
		s.writeError(w, &userError{msg: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	v, err := s.svc.Sets(ctx)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleRunSet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	run, err := s.svc.RunSet(ctx, r.PathValue("name"), r.PathValue("action"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

type resolvedEntry struct {
	file     string
	services []string
}

func resolveEntries(projects []service.Project, targets []string) ([]resolvedEntry, error) {
	entries, err := service.Resolve(projects, targets)
	if err != nil {
		return nil, &userError{msg: err.Error()}
	}
	out := make([]resolvedEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, resolvedEntry{file: e.Project.File, services: e.Services})
	}
	return out, nil
}
