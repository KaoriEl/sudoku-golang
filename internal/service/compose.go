package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sudoku-golang/internal/containers"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/engine"
	"sudoku-golang/internal/health"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/portcheck"
	"sudoku-golang/internal/prefs"
	"sudoku-golang/internal/shellquote"
	"sync"
	"time"

	"github.com/alperdrsnn/clime"
	"github.com/gammazero/workerpool"
)

const (
	cmdTimeout   = 30 * time.Minute
	outputTail   = 8 * 1024
	listTimeout  = 30 * time.Second
	maxTargetMsg = 5
)

// ErrFailed is returned when at least one compose file failed; the details were already printed.
var ErrFailed = errors.New("some compose files failed")

// Options select what a CLI command acts on.
type Options struct {
	ForceRebuild bool
	Debug        bool
	// Targets / Sets narrow the command to some projects or services (see Resolve); empty = everything.
	Targets []string
	Sets    []string
	// Wait overrides startup.waitHealthy from admin.yaml (nil = use the file).
	Wait *bool
	// NoPreflight skips the host port conflict check before `up`.
	NoPreflight bool
}

// Composer runs compose commands for the CLI. Commands are executed directly (no shell), so the same
// code works on Linux, macOS and Windows and paths with spaces or quotes are safe.
type Composer struct {
	log      *slog.Logger
	cfg      *configs.Config
	runner   *docker.Runner
	opts     Options
	entries  []Entry
	disabled *DisabledStore
	prefs    prefs.Prefs

	progressBar     *clime.ProgressBar
	currentProgress int64
	totalProgress   int64
	mu              sync.Mutex
	failed          int

	sourceOnce sync.Once
	source     *containers.Source
}

func NewComposer(log *slog.Logger, cfg *configs.Config, opts Options) (*Composer, error) {
	c := &Composer{
		log:      log,
		cfg:      cfg,
		runner:   docker.NewRunner(cfg.DockerPath, cfg.DockerComposePath, os.Environ()),
		opts:     opts,
		disabled: NewDisabledStore(LayoutOf(cfg)),
	}
	l := LayoutOf(cfg)
	if p, err := prefs.Load(prefs.Path(l.Data)); err == nil {
		c.prefs = p.Normalized()
	} else {
		c.prefs = prefs.Prefs{}.Normalized()
	}
	files, err := l.Discover()
	if err != nil {
		return nil, err
	}
	projects := l.Projects(files)
	targets := append([]string{}, opts.Targets...)
	if len(opts.Sets) > 0 {
		fromSets, err := NewSetsStore(l).Expand(opts.Sets)
		if err != nil {
			return nil, err
		}
		targets = append(targets, fromSets...)
	}
	if len(targets) == 0 {
		for _, p := range projects {
			c.entries = append(c.entries, Entry{Project: p})
		}
		return c, nil
	}
	if NeedsServices(projects, targets) {
		for i := range projects {
			projects[i].Services = c.Services(projects[i].File)
		}
	}
	if c.entries, err = Resolve(projects, targets); err != nil {
		return nil, err
	}
	return c, nil
}

// Entries is what the command will act on, in order.
func (c *Composer) Entries() []Entry {
	return c.entries
}

// Runner exposes the docker/compose runner for one-off commands (logs, exec).
func (c *Composer) Runner() *docker.Runner {
	return c.runner
}

// Services lists the services of a compose file (`compose config --services`).
func (c *Composer) Services(file string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()
	out, err := c.runner.Compose(ctx, file, "config", "--services")
	if err != nil {
		c.log.Error("Не удалось получить список сервисов", slog.Any("error", err), slog.String("composeFile", file))
		return nil
	}
	var services []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.Contains(line, " ") {
			services = append(services, line)
		}
	}
	return services
}

// Source lists containers over the engine API (lazily resolved), falling back to the CLI.
func (c *Composer) Source() *containers.Source {
	c.sourceOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
		defer cancel()
		eng := engine.New(engine.ResolveEndpoint(ctx, c.runner.DockerArgv(), c.runner.Env()))
		c.source = containers.NewSource(c.log, eng, c.runner)
	})
	return c.source
}

func (c *Composer) setProgressBar(count int) {
	c.totalProgress = int64(count)
	c.currentProgress = 0
	if c.opts.Debug || count == 0 {
		c.progressBar = nil
		return
	}
	c.progressBar = clime.NewProgressBar(c.totalProgress).
		WithLabel("Project processing...").
		WithStyle(clime.ProgressStyleModern).
		WithColor(clime.CyanColor).
		ShowRate(true)
	c.progressBar.Set(0)
	c.progressBar.Print()
}

func (c *Composer) addProgress(finishMessage string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentProgress = min(c.currentProgress+1, c.totalProgress)
	if c.progressBar != nil {
		c.progressBar.Set(c.currentProgress)
		c.progressBar.Print()
	} else {
		c.log.Info("Progress update", "current", c.currentProgress, "total", c.totalProgress)
	}
	if c.currentProgress == c.totalProgress {
		c.log.Info(finishMessage)
	}
}

func (c *Composer) result() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failed > 0 {
		return fmt.Errorf("%w: %d", ErrFailed, c.failed)
	}
	return nil
}

func (c *Composer) display(file string, args []string) string {
	argv := c.runner.ComposeArgv()
	if len(argv) > 1 && argv[0] == "/usr/bin/env" {
		argv = argv[1:]
	}
	return shellquote.Join(append(append(argv, "--file", file), args...))
}

func (c *Composer) runOne(e Entry, errMsg string, args []string) bool {
	file := e.Project.File
	if c.opts.Debug {
		cmd := c.display(file, args)
		c.log.Info("Executing command", "execCmd", cmd)
		clime.InfoLine("[debug] " + cmd)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	out := &docker.TailBuffer{Limit: outputTail}
	if err := c.runner.ComposeTo(ctx, file, out, args...); err != nil {
		c.mu.Lock()
		c.failed++
		c.mu.Unlock()
		c.log.Error(errMsg, slog.Any("error", err), slog.String("composeFile", file), slog.String("execCmd", c.display(file, args)))
		clime.ErrorLine(fmt.Sprintf("%s: %s\n%s", errMsg, e.Project.Rel, docker.LastLines(out.String(), 1500)))
		return false
	}
	return true
}

// serviceArgs returns the services for up/build: an explicit selection as is (a selected disabled
// service is started on purpose), a whole file minus its disabled services.
func (c *Composer) serviceArgs(e Entry) ([]string, bool) {
	if e.Services != nil {
		return e.Services, true
	}
	return c.enabledServices(e.Project.File)
}

func (c *Composer) Build() error {
	c.log.Info("Сборка контейнеров")
	c.setProgressBar(len(c.entries))
	for _, e := range c.entries {
		if services, run := c.serviceArgs(e); run {
			args := []string{"build"}
			if c.opts.ForceRebuild {
				args = append(args, "--no-cache")
			}
			c.runOne(e, "Ошибка выполнения команды сборки", append(args, services...))
		}
		c.addProgress("Сборка окончена")
	}
	return c.result()
}

func (c *Composer) waitEnabled() bool {
	if c.opts.Wait != nil {
		return *c.opts.Wait
	}
	return c.prefs.Startup.Wait()
}

func (c *Composer) Start() error {
	c.log.Info("Запуск контейнеров")
	if !c.opts.NoPreflight {
		c.Preflight()
	}
	c.setProgressBar(len(c.entries))
	for i, e := range c.entries {
		services, run := c.serviceArgs(e)
		ok := false
		if run {
			ok = c.runOne(e, "Ошибка выполнения команды запуска", append([]string{"up", "-d"}, services...))
		}
		c.addProgress("Все контейнеры запущены")
		if ok && e.Project.Infra && i < len(c.entries)-1 && c.waitEnabled() {
			c.waitHealthy(e, services)
		}
	}
	return c.result()
}

// waitHealthy blocks until the infrastructure's healthchecks pass so projects started next can
// connect to their databases right away. A timeout is reported but does not abort the start.
func (c *Composer) waitHealthy(e Entry, services []string) {
	timeout := c.prefs.Startup.Timeout()
	clime.InfoLine(fmt.Sprintf("Жду healthcheck инфраструктуры (до %s)…", timeout))
	ctx, cancel := context.WithTimeout(context.Background(), timeout+listTimeout)
	defer cancel()
	last := ""
	res, err := health.Wait(ctx, c.Source().List, e.Project.File, services, timeout, health.DefaultPoll, func(r health.Result) {
		if line := strings.Join(r.Pending, ", "); line != "" && line != last && c.opts.Debug {
			last = line
			clime.InfoLine("  ждём: " + line)
		}
	})
	if err != nil {
		clime.WarningLine("Инфраструктура не готова: " + res.Summary() + " — продолжаю запуск проектов")
		return
	}
	clime.SuccessLine("Инфраструктура готова: " + res.Summary())
}

// Preflight prints host port conflicts for the files about to be started.
func (c *Composer) Preflight() {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()
	running, err := c.Source().List(ctx)
	if err != nil {
		return
	}
	var conflicts []portcheck.Conflict
	for _, e := range c.entries {
		raw, err := c.runner.ComposeConfigJSON(ctx, e.Project.File)
		if err != nil {
			continue
		}
		bindings, err := portcheck.Published(raw, e.Project.File, e.Services)
		if err != nil {
			continue
		}
		conflicts = append(conflicts, portcheck.Check(ctx, bindings, running)...)
	}
	for _, cf := range conflicts {
		clime.WarningLine(cf.String())
	}
}

func (c *Composer) Stop() error {
	c.log.Info("Остановка контейнеров")
	return c.runParallel("Ошибка выполнения команды остановки", "Все контейнеры остановлены", func(e Entry) []string {
		return append([]string{"stop"}, e.Services...)
	})
}

func (c *Composer) Down() error {
	c.log.Info("Остановка и удаление контейнеров")
	return c.runParallel("Ошибка выполнения команды остановки и удаления", "Все контейнеры остановлены и удалены", func(e Entry) []string {
		if e.Services != nil {
			// `down` takes no services on older compose versions: stop + remove just the selected ones.
			return append([]string{"rm", "--stop", "--force"}, e.Services...)
		}
		return []string{"down"}
	})
}

// runParallel runs one compose command per file through a fresh worker pool (MAX_WORKERS).
func (c *Composer) runParallel(errMsg, doneMsg string, args func(Entry) []string) error {
	c.setProgressBar(len(c.entries))
	wp := workerpool.New(max(1, c.cfg.MaxWorkers))
	for _, e := range c.entries {
		wp.Submit(func() {
			c.runOne(e, errMsg, args(e))
			c.addProgress(doneMsg)
		})
	}
	wp.StopWait()
	return c.result()
}

// enabledServices returns the services to pass to up/build (nil = all of them) and whether the file
// should be processed at all (false when every service is disabled).
func (c *Composer) enabledServices(composeFile string) ([]string, bool) {
	disabled, err := c.disabled.For(composeFile)
	if err != nil {
		c.log.Error("Не удалось прочитать список отключённых сервисов", slog.Any("error", err))
		return nil, true
	}
	if len(disabled) == 0 {
		return nil, true
	}
	all := c.Services(composeFile)
	if all == nil {
		return nil, true
	}
	enabled := EnabledServices(all, disabled)
	skipped := strings.Join(disabled, ", ")
	if len(enabled) == 0 {
		clime.InfoLine(fmt.Sprintf("Пропущен %s: все сервисы отключены (%s)", c.disabled.Rel(composeFile), skipped))
		return nil, false
	}
	clime.InfoLine(fmt.Sprintf("%s: пропускаю отключённые сервисы %s", c.disabled.Rel(composeFile), skipped))
	return enabled, true
}

// Describe is a one-line summary of the selection for the CLI header.
func (c *Composer) Describe() string {
	if len(c.entries) == 0 {
		return "ничего не выбрано"
	}
	names := make([]string, 0, len(c.entries))
	for _, e := range c.entries {
		n := e.Project.Name
		if e.Services != nil {
			n += "/" + strings.Join(e.Services, ",")
		}
		names = append(names, n)
	}
	if len(names) > maxTargetMsg {
		names = append(names[:maxTargetMsg], fmt.Sprintf("и ещё %d", len(names)-maxTargetMsg))
	}
	return strings.Join(names, " · ")
}
