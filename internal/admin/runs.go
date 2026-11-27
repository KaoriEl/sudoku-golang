package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"strings"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/health"
	"sudoku-golang/internal/service"
	"sync"
	"time"
)

const (
	actionStart   = "start"
	actionStop    = "stop"
	actionRestart = "restart"

	phaseUp   = "up"
	phaseWait = "wait"

	runServiceRebuild = "service-rebuild"
	runApplyPorts     = "apply-ports"

	maxOutputSize  = 16 * 1024
	maxRunsKept    = 20
	stepTimeout    = 30 * time.Minute
	outputInterval = time.Second
)

// stepFunc is a step implemented in Go (healthcheck wait, database dump) instead of a compose command.
type stepFunc func(ctx context.Context, out io.Writer) error

type stepPlan struct {
	file       ComposeFile
	phase      string
	args       []string
	parallel   bool
	skipReason string
	fn         stepFunc
	display    string
}

type runPlan struct {
	action string
	target string
	steps  []stepPlan
}

func upStep(f ComposeFile, services []string) stepPlan {
	return stepPlan{file: f, phase: phaseUp, args: append([]string{"up", "-d"}, services...)}
}

func (s *Service) bulkPlan(action string, files []ComposeFile) (runPlan, bool) {
	plan := runPlan{action: action}
	stop := func() {
		for _, f := range files {
			plan.steps = append(plan.steps, stepPlan{file: f, phase: actionStop, args: []string{actionStop}, parallel: true})
		}
	}
	up := func() {
		for _, f := range files {
			st := upStep(f, nil)
			if len(f.Disabled) > 0 && len(f.Services) > 0 {
				enabled := service.EnabledServices(f.Services, f.Disabled)
				st.args = append(st.args, enabled...)
				if len(enabled) == 0 {
					st.skipReason = "все сервисы отключены: " + strings.Join(f.Disabled, ", ")
				}
			}
			plan.steps = append(plan.steps, st)
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
	plan.steps = s.withHealthWait(plan.steps)
	return plan, true
}

// withHealthWait inserts "wait until the infrastructure is healthy" right after the infra `up -d`
// when projects are started after it, so a project does not crash-loop on a database that is still
// booting. Disabled via startup.waitHealthy=false in admin.yaml.
func (s *Service) withHealthWait(steps []stepPlan) []stepPlan {
	p, _ := s.prefs.Get()
	if !p.Startup.Wait() {
		return steps
	}
	infraIdx := -1
	for i, st := range steps {
		if st.phase == phaseUp && st.file.Infra && st.skipReason == "" {
			infraIdx = i
		}
	}
	if infraIdx < 0 {
		return steps
	}
	later := false
	for _, st := range steps[infraIdx+1:] {
		if st.phase == phaseUp && !st.file.Infra {
			later = true
		}
	}
	if !later {
		return steps
	}
	infra := steps[infraIdx]
	services := infra.args[2:]
	timeout := p.Startup.Timeout()
	wait := stepPlan{
		file:    infra.file,
		phase:   phaseWait,
		display: "wait healthy " + infra.file.Rel + " (≤ " + timeout.String() + ")",
		fn: func(ctx context.Context, out io.Writer) error {
			last := ""
			res, err := health.Wait(ctx, s.source.List, infra.file.Path, services, timeout, health.DefaultPoll, func(r health.Result) {
				if line := strings.Join(r.Pending, ", "); line != "" && line != last {
					last = line
					_, _ = io.WriteString(out, "ждём: "+line+"\n")
				}
			})
			_, _ = io.WriteString(out, res.Summary()+"\n")
			return err //nolint:wrapcheck // health errors are the step result
		},
	}
	out := make([]stepPlan, 0, len(steps)+1)
	out = append(out, steps[:infraIdx+1]...)
	out = append(out, wait)
	return append(out, steps[infraIdx+1:]...)
}

type runManager struct {
	mu      sync.Mutex
	runs    map[string]*Run
	order   []string
	current string
	// exclusive is held by an operation that must not overlap any run (disk cleanup, a config save
	// that re-executes the server); runs and container actions are refused while it is held.
	exclusive bool

	history  *runHistory
	onChange func(id string, finished bool)
}

func newRunManager(h *runHistory, onChange func(string, bool)) *runManager {
	m := &runManager{runs: map[string]*Run{}, history: h, onChange: onChange}
	if h != nil {
		for _, r := range h.recent(maxRunsKept) {
			m.runs[r.ID] = r
			m.order = append(m.order, r.ID)
		}
	}
	return m
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (m *runManager) changed(id string, finished bool) {
	if m.onChange != nil {
		m.onChange(id, finished)
	}
}

func (m *runManager) busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current != "" || m.exclusive
}

// tryExclusive takes the exclusive slot; it fails with ErrRunInProgress while a run is active and
// with errExclusiveBusy while another exclusive operation holds it.
func (m *runManager) tryExclusive() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case m.current != "":
		return ErrRunInProgress
	case m.exclusive:
		return errExclusiveBusy
	}
	m.exclusive = true
	return nil
}

func (m *runManager) endExclusive() {
	m.mu.Lock()
	m.exclusive = false
	m.mu.Unlock()
}

func (m *runManager) begin(plan runPlan, display func([]string) string) (*Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != "" {
		return nil, ErrRunInProgress
	}
	if m.exclusive {
		return nil, errExclusiveBusy
	}

	run := &Run{ID: newID(), Action: plan.action, Target: plan.target, Status: RunRunning, StartedAt: time.Now().UTC()}
	for _, st := range plan.steps {
		cmd := st.display
		if st.fn == nil {
			cmd = display(append([]string{flagFile, st.file.Path}, st.args...))
		}
		run.Steps = append(run.Steps, Step{
			ComposeFile: st.file.Path,
			Rel:         st.file.Rel,
			Phase:       st.phase,
			Command:     cmd,
			Status:      StepPending,
		})
	}

	m.runs[run.ID] = run
	m.order = append(m.order, run.ID)
	if len(m.order) > maxRunsKept {
		delete(m.runs, m.order[0])
		m.order = m.order[1:]
	}
	m.current = run.ID
	return run, nil
}

func cloneRun(r *Run) *Run {
	c := *r
	c.Steps = append([]Step(nil), r.Steps...)
	return &c
}

func (m *runManager) get(id string) *Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil
	}
	return cloneRun(r)
}

// latestSummary is the run without step output: /api/state is polled and pushed often and only
// needs ids and statuses, while full output (up to 16 KB per step) is served by /api/runs/{id}.
func (m *runManager) latestSummary() *Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.order) == 0 {
		return nil
	}
	r := m.runs[m.order[len(m.order)-1]]
	c := *r
	c.Steps = make([]Step, len(r.Steps))
	for i, st := range r.Steps {
		st.Output, st.Error = "", ""
		c.Steps[i] = st
	}
	return &c
}

func (m *runManager) updateStep(runID string, idx int, fn func(*Step)) {
	m.mu.Lock()
	if r, ok := m.runs[runID]; ok && idx < len(r.Steps) {
		fn(&r.Steps[idx])
	}
	m.mu.Unlock()
	m.changed(runID, false)
}

func (m *runManager) finish(runID string) {
	m.mu.Lock()
	r, ok := m.runs[runID]
	if !ok {
		m.mu.Unlock()
		return
	}
	failed, skipped := 0, 0
	for i := range r.Steps {
		switch r.Steps[i].Status {
		case StepFailed:
			failed++
		case StepPending:
			r.Steps[i].Status = StepSkipped
			skipped++
		case StepRunning, StepSuccess, StepSkipped:
		}
	}
	switch {
	case failed == 0 && skipped == 0:
		r.Status = RunSuccess
	case failed+skipped == len(r.Steps):
		r.Status = RunFailed
	default:
		r.Status = RunPartial
	}
	now := time.Now().UTC()
	r.FinishedAt = &now
	m.current = ""
	done := cloneRun(r)
	m.mu.Unlock()

	if m.history != nil {
		m.history.save(done)
	}
	m.changed(runID, true)
}

func (s *Service) onRunChange(id string, finished bool) {
	s.hub.markRun(id)
	if finished {
		s.hub.markState()
	}
}

func (s *Service) displayCompose(args []string) string {
	return shellJoin(append(displayArgv(s.docker.ComposeArgv()), args...))
}

func (s *Service) startRun(plan runPlan, stopOnFailure bool) (*Run, error) {
	run, err := s.runs.begin(plan, s.displayCompose)
	if err != nil {
		return nil, err
	}
	s.log.Info("run started", slog.String("action", plan.action), slog.String("target", plan.target), slog.Int("steps", len(plan.steps)))
	go s.execute(s.runCtx, run.ID, plan, stopOnFailure)
	s.hub.markState()
	return s.runs.get(run.ID), nil
}

// liveOutput copies the step's output tail into the run every second while the step executes, so
// the run drawer shows progress instead of a blank panel until the step ends.
func (s *Service) liveOutput(ctx context.Context, runID string, idx int, tb *docker.TailBuffer) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(outputInterval)
		defer t.Stop()
		var seen int64
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n := tb.Written(); n != seen {
					seen = n
					out := strings.TrimSpace(tb.String())
					s.runs.updateStep(runID, idx, func(step *Step) { step.Output = out })
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (s *Service) runStep(ctx context.Context, runID string, idx int, st stepPlan) bool {
	start := time.Now().UTC()
	if st.skipReason != "" {
		s.runs.updateStep(runID, idx, func(step *Step) {
			step.Status = StepSkipped
			step.Output = st.skipReason
			step.StartedAt = &start
			step.FinishedAt = &start
		})
		return true
	}
	s.runs.updateStep(runID, idx, func(step *Step) {
		step.Status = StepRunning
		step.StartedAt = &start
	})

	stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	tb := &docker.TailBuffer{Limit: maxOutputSize}
	stopLive := s.liveOutput(stepCtx, runID, idx, tb)
	var err error
	if st.fn != nil {
		err = st.fn(stepCtx, tb)
	} else {
		err = s.docker.ComposeTo(stepCtx, st.file.Path, tb, st.args...)
	}
	stopLive()
	out := strings.TrimSpace(tb.String())

	end := time.Now().UTC()
	s.runs.updateStep(runID, idx, func(step *Step) {
		step.Output = out
		step.FinishedAt = &end
		if err != nil {
			step.Status = StepFailed
			step.Error = err.Error()
			return
		}
		step.Status = StepSuccess
	})
	if err != nil {
		s.log.Error("run step failed", slog.String("file", st.file.Path), slog.String("phase", st.phase), slog.Any("error", err))
	}
	return err == nil
}

func (s *Service) execute(ctx context.Context, runID string, plan runPlan, stopOnFailure bool) {
	defer s.runs.finish(runID)

	workers := max(1, s.cfg.MaxWorkers)
	for i := 0; i < len(plan.steps); {
		if plan.steps[i].parallel {
			j := i
			for j < len(plan.steps) && plan.steps[j].parallel && plan.steps[j].phase == plan.steps[i].phase {
				j++
			}
			sem := make(chan struct{}, workers)
			var wg sync.WaitGroup
			for k := i; k < j; k++ {
				wg.Add(1)
				sem <- struct{}{}
				go func(k int) {
					defer wg.Done()
					defer func() { <-sem }()
					s.runStep(ctx, runID, k, plan.steps[k])
				}(k)
			}
			wg.Wait()
			i = j
			continue
		}

		if ok := s.runStep(ctx, runID, i, plan.steps[i]); !ok && stopOnFailure {
			return
		}
		i++
	}
}
