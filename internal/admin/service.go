package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sudoku-golang/internal/backup"
	"sudoku-golang/internal/containers"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/engine"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/metrics"
	"sudoku-golang/internal/paths"
	"sudoku-golang/internal/prefs"
	"sudoku-golang/internal/service"
	"sudoku-golang/internal/updates"
	"sync"
	"time"
)

var (
	ErrUnknownAction     = errors.New("unknown action")
	ErrContainerNotFound = errors.New("container not found")
	ErrNotManaged        = errors.New("container does not belong to sudoku compose files")
	ErrRunInProgress     = errors.New("another bulk action is in progress")
)

const stopTimeoutSeconds = 10

var containerActions = map[string]bool{actionStop: true, actionStart: true, actionRestart: true}

type Service struct {
	log      *slog.Logger
	cfg      *configs.Config
	docker   *docker.Runner
	source   *containers.Source
	engine   *engine.Engine
	runs     *runManager
	files    sync.Mutex
	runCtx   context.Context
	addr     string
	layout   service.Layout
	disabled *service.DisabledStore
	sets     *service.SetsStore
	svcCache *servicesCache
	cfgCache configCache
	metrics  *metrics.Collector
	prefs    *prefs.Store

	hub       *hub
	alerts    *alerter
	history   *runHistory
	updates   *updates.Checker
	backups   *backup.Manager
	scheduler *scheduler
	df        diskCache

	authToken  string
	unixSocket bool

	selfUpd    selfUpdater
	exe        func() (string, error)
	selfUpdate selfUpdateState

	restart      Restarter
	addrFromFlag bool
}

func NewService(log *slog.Logger, cfg *configs.Config, runner *docker.Runner, collector *metrics.Collector) *Service {
	eng := collector.Engine()
	layout := service.LayoutOf(cfg)
	s := &Service{
		log:      log,
		cfg:      cfg,
		docker:   runner,
		source:   containers.NewSource(log, eng, runner),
		engine:   eng,
		runCtx:   context.Background(),
		layout:   layout,
		disabled: service.NewDisabledStore(layout),
		sets:     service.NewSetsStore(layout),
		svcCache: &servicesCache{entries: map[string]servicesEntry{}},
		metrics:  collector,
		prefs:    prefs.NewStore(layout.Data),
		hub:      newHub(),
		history:  newRunHistory(log, layout.ConfigDir()),
		updates:  updates.NewChecker(eng),
	}
	s.runs = newRunManager(s.history, s.onRunChange)
	s.alerts = newAlerter(s)
	s.backups = backup.NewManager(log, eng, runner, func() string {
		p, _ := s.prefs.Get()
		return p.BackupDir(layout.Data)
	})
	s.scheduler = newScheduler(s)
	p, err := s.prefs.Get()
	if err != nil {
		log.Warn("admin prefs ignored", slog.Any("error", err))
	}
	s.applyMetricsPrefs(p)
	return s
}

// Start runs the background workers (Docker events, schedules) until ctx ends.
func (s *Service) Start(ctx context.Context) {
	s.runCtx = ctx
	s.hub.start(ctx, s)
	go s.watchEvents(ctx)
	go s.scheduler.run(ctx)
}

// SetAuth configures the token check and whether the server listens on a unix socket.
func (s *Service) SetAuth(token string, unixSocket bool) {
	s.authToken = token
	s.unixSocket = unixSocket
}

func (s *Service) composeFiles() ([]ComposeFile, map[string]string, error) {
	found, err := s.layout.Discover()
	if err != nil {
		return nil, nil, fmt.Errorf("discover compose files: %w", err)
	}

	files := make([]ComposeFile, 0, len(found))
	known := make(map[string]string, len(found))
	for _, p := range found {
		project := filepath.Base(filepath.Dir(p))
		files = append(files, ComposeFile{Path: p, Rel: s.layout.Rel(p), Project: project, Infra: s.layout.IsInfra(p)})
		known[paths.Normalize(p)] = p
	}
	return files, known, nil
}

func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	files, known, err := s.composeFiles()
	if err != nil {
		return Snapshot{}, err
	}

	raw, err := s.source.List(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list containers: %w", err)
	}

	files = s.withServices(ctx, files)
	disabledBy := map[string]map[string]bool{}
	for _, f := range files {
		disabledBy[f.Path] = map[string]bool{}
		for _, d := range f.Disabled {
			disabledBy[f.Path][d] = true
		}
	}

	updatesBy := s.updates.Results()
	list := make([]Container, 0, len(raw))
	for _, c := range raw {
		mc := mapContainer(c, known)
		mc.Disabled = mc.Managed && disabledBy[mc.ComposeFile][mc.Service]
		if u, ok := updatesBy[mc.Image]; ok {
			mc.Update = &u
		}
		list = append(list, mc)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].ComposeFile != list[j].ComposeFile {
			return list[i].ComposeFile < list[j].ComposeFile
		}
		return list[i].Name < list[j].Name
	})

	return Snapshot{
		Root:         s.layout.Root,
		ComposeFiles: files,
		Containers:   list,
		Run:          s.runs.latestSummary(),
		MaxWorkers:   s.cfg.MaxWorkers,
		DisabledPath: s.disabled.Path(),
		GeneratedAt:  time.Now().UTC(),
		Live:         s.hub.eventsLive(),
	}, nil
}

func (s *Service) FindContainer(ctx context.Context, id string) (Container, error) {
	_, known, err := s.composeFiles()
	if err != nil {
		return Container{}, err
	}
	found, err := s.source.Inspect(ctx, id)
	if errors.Is(err, containers.ErrNotFound) {
		return Container{}, ErrContainerNotFound
	}
	if err != nil {
		return Container{}, err //nolint:wrapcheck // already descriptive
	}
	if _, ok := found.Config.Labels[labelProject]; !ok {
		return Container{}, ErrContainerNotFound
	}
	return mapContainer(found, known), nil
}

func (s *Service) ContainerAction(ctx context.Context, id, action string) (Container, error) {
	if !containerActions[action] {
		return Container{}, ErrUnknownAction
	}
	if s.runs.busy() {
		return Container{}, ErrRunInProgress
	}

	c, err := s.FindContainer(ctx, id)
	if err != nil {
		return Container{}, err
	}
	if !c.Managed {
		return Container{}, ErrNotManaged
	}

	s.log.Info("container action", slog.String("action", action), slog.String("container", c.Name))
	s.alerts.expect(c.ID)
	defer s.source.Forget(c.ID)
	err = s.engine.ContainerAction(ctx, c.ID, action, stopTimeoutSeconds)
	if err != nil && !engine.IsEngineError(err) {
		err = s.docker.ContainerAction(ctx, c.ID, action)
	}
	if err != nil {
		return Container{}, fmt.Errorf("docker %s %s: %w", action, c.Name, err)
	}

	if updated, inspectErr := s.FindContainer(ctx, c.ID); inspectErr == nil {
		c = updated
	}
	s.hub.markState()
	return c, nil
}

func (s *Service) StartBulk(ctx context.Context, action string) (*Run, error) {
	files, _, err := s.composeFiles()
	if err != nil {
		return nil, err
	}
	files = s.withServices(ctx, files)
	plan, ok := s.bulkPlan(action, files)
	if !ok {
		return nil, ErrUnknownAction
	}
	return s.startRun(plan, false)
}

func (s *Service) Run(id string) *Run {
	if r := s.runs.get(id); r != nil {
		return r
	}
	return s.history.load(id)
}
