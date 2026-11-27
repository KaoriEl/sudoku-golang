package admin

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strconv"
)

const (
	selectionDisable = "disable"
	selectionEnable  = "enable"
	runSelection     = "selection-"
	maxSelection     = 500
)

type SelectionTarget struct {
	ComposeFile string `json:"composeFile"`
	Service     string `json:"service"`
}

type SelectionRequest struct {
	Action  string            `json:"action"`
	Targets []SelectionTarget `json:"targets"`
}

type SelectionResult struct {
	Run        *Run           `json:"run"`
	Toggled    []ToggleResult `json:"toggled"`
	Dependents []string       `json:"dependents"`
	StorePath  string         `json:"storePath"`
}

// selectionGroups validates targets against discovered compose files and their services and groups
// them by file, keeping discovery order and de-duplicating services.
func (s *Service) selectionGroups(ctx context.Context, targets []SelectionTarget) ([]ComposeFile, map[string][]string, error) {
	if len(targets) == 0 || len(targets) > maxSelection {
		return nil, nil, &userError{msg: "выберите от 1 до " + strconv.Itoa(maxSelection) + " сервисов"}
	}
	files, _, err := s.composeFiles()
	if err != nil {
		return nil, nil, err
	}
	files = s.withServices(ctx, files)
	byPath := map[string]ComposeFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	groups := map[string][]string{}
	for _, t := range targets {
		f, ok := byPath[t.ComposeFile]
		if !ok {
			return nil, nil, fmt.Errorf("%w: %s", ErrUnknownService, t.ComposeFile)
		}
		if !slices.Contains(f.Services, t.Service) {
			return nil, nil, fmt.Errorf("%w: %s (%s)", ErrUnknownService, t.Service, f.Rel)
		}
		if !slices.Contains(groups[f.Path], t.Service) {
			groups[f.Path] = append(groups[f.Path], t.Service)
		}
	}
	ordered := make([]ComposeFile, 0, len(groups))
	for _, f := range files {
		if svcs, ok := groups[f.Path]; ok {
			sort.Strings(svcs)
			ordered = append(ordered, f)
		}
	}
	return ordered, groups, nil
}

func selectionPlan(action string, files []ComposeFile, groups map[string][]string, count int) (runPlan, bool) {
	plan := runPlan{action: runSelection + action, target: strconv.Itoa(count)}
	stop := func() {
		for _, f := range files {
			plan.steps = append(plan.steps, stepPlan{file: f, phase: actionStop, args: append([]string{actionStop}, groups[f.Path]...), parallel: true})
		}
	}
	up := func() {
		for _, f := range files {
			plan.steps = append(plan.steps, upStep(f, groups[f.Path]))
		}
	}
	switch action {
	case actionStart:
		up()
	case actionStop:
		stop()
	case actionRestart:
		stop()
		up()
	default:
		return plan, false
	}
	return plan, true
}

// ApplySelection runs start/stop/restart for the selected services as one run (explicitly selected
// services are started even if they are flagged "don't start"), or flips the flag for all of them.
func (s *Service) ApplySelection(ctx context.Context, req SelectionRequest) (SelectionResult, error) {
	files, groups, err := s.selectionGroups(ctx, req.Targets)
	if err != nil {
		return SelectionResult{}, err
	}
	count := 0
	for _, svcs := range groups {
		count += len(svcs)
	}
	res := SelectionResult{Toggled: []ToggleResult{}, Dependents: []string{}, StorePath: s.disabled.Path()}

	if req.Action == selectionDisable || req.Action == selectionEnable {
		if err := s.toggleSelection(ctx, req.Action == selectionDisable, files, groups, &res); err != nil {
			return res, err
		}
		s.log.Info("selection autostart toggled", slog.String("action", req.Action), slog.Int("services", count))
		return res, nil
	}

	plan, ok := selectionPlan(req.Action, files, groups, count)
	if !ok {
		return res, ErrUnknownAction
	}
	plan.steps = s.withHealthWait(plan.steps)
	run, err := s.startRun(plan, false)
	if err != nil {
		return res, err
	}
	res.Run = run
	return res, nil
}

func (s *Service) toggleSelection(ctx context.Context, disable bool, files []ComposeFile, groups map[string][]string, res *SelectionResult) error {
	store := s.disabled.Path()
	for _, f := range files {
		for _, svc := range groups[f.Path] {
			if err := s.disabled.Set(f.Path, svc, disable); err != nil {
				return fmt.Errorf("save disabled: %w", err)
			}
			res.Toggled = append(res.Toggled, ToggleResult{ComposeFile: f.Path, Service: svc, Disabled: disable, Dependents: []string{}, StorePath: store})
		}
		if !disable {
			continue
		}
		disabled, _ := s.disabled.For(f.Path)
		for _, svc := range groups[f.Path] {
			for _, d := range s.dependents(ctx, f.Path, svc, disabled) {
				if dep := d + " → " + svc; !slices.Contains(res.Dependents, dep) {
					res.Dependents = append(res.Dependents, dep)
				}
			}
		}
	}
	return nil
}
