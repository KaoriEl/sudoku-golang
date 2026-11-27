package admin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sudoku-golang/internal/autostart"
	"sudoku-golang/internal/cleanup"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/metrics"
	"sudoku-golang/internal/notify"
	"sudoku-golang/internal/prefs"
	"sudoku-golang/internal/version"
	"time"
)

type Settings struct {
	Root          string               `json:"root"`
	DockerPath    string               `json:"dockerPath"`
	ComposePath   string               `json:"composePath"`
	MaxWorkers    int                  `json:"maxWorkers"`
	Addr          string               `json:"addr"`
	Autostart     autostart.Status     `json:"autostart"`
	Metrics       MetricsSettings      `json:"metrics"`
	Startup       StartupSettings      `json:"startup"`
	Notifications NotificationSettings `json:"notifications"`
	Schedule      ScheduleSettings     `json:"schedule"`
	Backups       BackupSettings       `json:"backups"`
	Version       version.Info         `json:"version"`
	Auth          AuthSettings         `json:"auth"`
	Endpoint      string               `json:"endpoint"`
	CanRestart    bool                 `json:"canRestart"`
}

type MetricsSettings struct {
	Background         bool   `json:"background"`
	Collecting         bool   `json:"collecting"`
	LiveInterval       int    `json:"liveInterval"`
	BackgroundInterval int    `json:"backgroundInterval"`
	PrefsPath          string `json:"prefsPath"`
}

type StartupSettings struct {
	WaitHealthy bool `json:"waitHealthy"`
	WaitTimeout int  `json:"waitTimeout"`
}

type NotificationSettings struct {
	Desktop          bool   `json:"desktop"`
	Browser          bool   `json:"browser"`
	DesktopSupported bool   `json:"desktopSupported"`
	DesktopReason    string `json:"desktopReason"`
}

type ScheduleSettings struct {
	Cleanup     prefs.CleanupSchedule `json:"cleanup"`
	Updates     prefs.UpdatesSchedule `json:"updates"`
	NextCleanup *time.Time            `json:"nextCleanup"`
	NextUpdates *time.Time            `json:"nextUpdates"`
	CacheModes  []string              `json:"cacheModes"`
}

type BackupSettings struct {
	Keep int    `json:"keep"`
	Dir  string `json:"dir"`
}

type AuthSettings struct {
	Token  bool `json:"token"`
	Socket bool `json:"socket"`
}

type MetricsUpdate struct {
	Background         *bool `json:"background"`
	LiveInterval       *int  `json:"liveInterval"`
	BackgroundInterval *int  `json:"backgroundInterval"`
}

// PrefsUpdate is a partial update of admin.yaml; nil fields stay as they are.
type PrefsUpdate struct {
	Startup *struct {
		WaitHealthy *bool `json:"waitHealthy"`
		WaitTimeout *int  `json:"waitTimeout"`
	} `json:"startup"`
	Notifications *struct {
		Desktop *bool `json:"desktop"`
		Browser *bool `json:"browser"`
	} `json:"notifications"`
	Cleanup *struct {
		Enabled           *bool   `json:"enabled"`
		EveryHours        *int    `json:"everyHours"`
		BuildCache        *string `json:"buildCache"`
		DanglingImages    *bool   `json:"danglingImages"`
		StoppedContainers *bool   `json:"stoppedContainers"`
	} `json:"cleanup"`
	Updates *struct {
		Enabled    *bool `json:"enabled"`
		EveryHours *int  `json:"everyHours"`
	} `json:"updates"`
	Backups *struct {
		Keep *int    `json:"keep"`
		Dir  *string `json:"dir"`
	} `json:"backups"`
}

var ErrBadInterval = errors.New("invalid interval")

func checkInterval(v *int, lo, hi time.Duration) error {
	if v == nil {
		return nil
	}
	d := time.Duration(*v) * time.Second
	if d < lo || d > hi {
		return fmt.Errorf("%w: %d с (допустимо %d–%d с)", ErrBadInterval, *v, int(lo.Seconds()), int(hi.Seconds()))
	}
	return nil
}

func checkRange(name string, v *int, lo, hi int) error {
	if v != nil && (*v < lo || *v > hi) {
		return &userError{msg: fmt.Sprintf("%s: от %d до %d", name, lo, hi)}
	}
	return nil
}

func (s *Service) autostartSpec() (autostart.Spec, string) {
	return s.autostartSpecWith(s.addrFromFlag)
}

// autostartSpecWith builds the autostart command. Settings come from .env in SUDOKU_HOME;
// --addr is passed only when the running server got it as a flag, and ROOT_PROJECTS_FOLDER only
// when there is no .env to read it from.
func (s *Service) autostartSpecWith(keepAddrFlag bool) (autostart.Spec, string) {
	extra := map[string]string{configs.HomeEnv: s.cfg.Home}
	if _, err := os.Stat(envPath()); err != nil {
		root, _ := filepath.Abs(s.cfg.RootProjectsFolder)
		extra[keyRoot] = root
	}
	addr := ""
	if keepAddrFlag {
		addr = s.addr
	}
	return autostart.CurrentSpec(addr, extra)
}

func next(last time.Time, everyHours int, enabled bool) *time.Time {
	if !enabled {
		return nil
	}
	n := last.Add(time.Duration(everyHours) * time.Hour)
	if last.IsZero() || n.Before(time.Now()) {
		n = time.Now().Add(scheduleTick)
	}
	n = n.UTC()
	return &n
}

func (s *Service) Settings() Settings {
	root, _ := filepath.Abs(s.cfg.RootProjectsFolder)
	spec, reason := s.autostartSpec()
	p, err := s.prefs.Get()
	if err != nil {
		s.log.Warn("admin prefs ignored", "error", err)
	}
	p = p.Normalized()
	supported, why := notify.Supported()
	return Settings{
		Root:        root,
		DockerPath:  s.cfg.DockerPath,
		ComposePath: s.cfg.DockerComposePath,
		MaxWorkers:  s.cfg.MaxWorkers,
		Addr:        s.addr,
		Autostart:   autostart.Get(spec, reason),
		Metrics:     s.metricsSettings(),
		Startup:     StartupSettings{WaitHealthy: p.Startup.Wait(), WaitTimeout: int(p.Startup.Timeout().Seconds())},
		Notifications: NotificationSettings{
			Desktop: p.Notifications.Desktop, Browser: p.Notifications.BrowserOn(),
			DesktopSupported: supported, DesktopReason: why,
		},
		Schedule: ScheduleSettings{
			Cleanup:     p.Schedule.Cleanup,
			Updates:     p.Schedule.Updates,
			NextCleanup: next(p.Schedule.Cleanup.LastRun, p.Schedule.Cleanup.EveryHours, p.Schedule.Cleanup.Enabled),
			NextUpdates: next(p.Schedule.Updates.LastRun, p.Schedule.Updates.EveryHours, p.Schedule.Updates.Enabled),
			CacheModes:  cleanup.CacheAges,
		},
		Backups:    BackupSettings{Keep: p.Backups.Keep, Dir: p.BackupDir(s.layout.Data)},
		Version:    version.Get(),
		Auth:       AuthSettings{Token: s.authToken != "", Socket: s.unixSocket},
		Endpoint:   s.engine.Socket(),
		CanRestart: s.restart != nil,
	}
}

func (s *Service) metricsSettings() MetricsSettings {
	live, bg := s.metrics.Intervals()
	return MetricsSettings{
		Background:         s.metrics.Background(),
		Collecting:         s.metrics.Active(),
		LiveInterval:       int(live.Seconds()),
		BackgroundInterval: int(bg.Seconds()),
		PrefsPath:          s.prefs.Path(),
	}
}

func (s *Service) UpdateMetrics(u MetricsUpdate) (Settings, error) {
	if err := checkInterval(u.LiveInterval, metrics.MinLiveInterval, metrics.MaxLiveInterval); err != nil {
		return Settings{}, err
	}
	if err := checkInterval(u.BackgroundInterval, metrics.MinBackgroundInterval, metrics.MaxBackgroundInterval); err != nil {
		return Settings{}, err
	}
	p, err := s.prefs.Update(func(p *prefs.Prefs) error {
		if u.Background != nil {
			p.Metrics.Background = *u.Background
		}
		if u.LiveInterval != nil {
			p.Metrics.LiveInterval = *u.LiveInterval
		}
		if u.BackgroundInterval != nil {
			p.Metrics.BackgroundInterval = *u.BackgroundInterval
		}
		return nil
	})
	if err != nil {
		return Settings{}, err //nolint:wrapcheck // already descriptive
	}
	s.applyMetricsPrefs(p)
	s.log.Info("metrics settings updated", "background", p.Metrics.Background,
		"liveInterval", p.Metrics.LiveInterval, "backgroundInterval", p.Metrics.BackgroundInterval)
	return s.Settings(), nil
}

func (s *Service) applyMetricsPrefs(p prefs.Prefs) {
	s.metrics.SetIntervals(time.Duration(p.Metrics.LiveInterval)*time.Second, time.Duration(p.Metrics.BackgroundInterval)*time.Second)
	s.metrics.SetBackground(p.Metrics.Background)
}

func validatePrefsUpdate(u PrefsUpdate) error {
	if u.Startup != nil {
		if err := checkRange("waitTimeout", u.Startup.WaitTimeout, prefs.MinWaitTimeout, prefs.MaxWaitTimeout); err != nil {
			return err
		}
	}
	if u.Cleanup != nil {
		if err := checkRange("everyHours", u.Cleanup.EveryHours, prefs.MinScheduleHours, prefs.MaxScheduleHours); err != nil {
			return err
		}
		if u.Cleanup.BuildCache != nil && *u.Cleanup.BuildCache != "" && *u.Cleanup.BuildCache != "off" &&
			!slices.Contains(cleanup.CacheAges, *u.Cleanup.BuildCache) {
			return &userError{msg: "buildCache: одно из off, " + fmt.Sprint(cleanup.CacheAges)}
		}
	}
	if u.Updates != nil {
		if err := checkRange("everyHours", u.Updates.EveryHours, prefs.MinScheduleHours, prefs.MaxScheduleHours); err != nil {
			return err
		}
	}
	if u.Backups != nil {
		if err := checkRange("keep", u.Backups.Keep, 1, prefs.MaxBackupsKeep); err != nil {
			return err
		}
	}
	return nil
}

func set[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

// UpdatePrefs applies a partial update of the startup / notification / schedule / backup settings.
func (s *Service) UpdatePrefs(u PrefsUpdate) (Settings, error) {
	if err := validatePrefsUpdate(u); err != nil {
		return Settings{}, err
	}
	if u.Notifications != nil && u.Notifications.Desktop != nil && *u.Notifications.Desktop {
		if ok, why := notify.Supported(); !ok {
			return Settings{}, &userError{msg: why}
		}
	}
	_, err := s.prefs.Update(func(p *prefs.Prefs) error {
		if u.Startup != nil {
			if u.Startup.WaitHealthy != nil {
				v := *u.Startup.WaitHealthy
				p.Startup.WaitHealthy = &v
			}
			set(&p.Startup.WaitTimeout, u.Startup.WaitTimeout)
		}
		if u.Notifications != nil {
			set(&p.Notifications.Desktop, u.Notifications.Desktop)
			if u.Notifications.Browser != nil {
				v := *u.Notifications.Browser
				p.Notifications.Browser = &v
			}
		}
		if c := u.Cleanup; c != nil {
			set(&p.Schedule.Cleanup.Enabled, c.Enabled)
			set(&p.Schedule.Cleanup.EveryHours, c.EveryHours)
			set(&p.Schedule.Cleanup.BuildCache, c.BuildCache)
			set(&p.Schedule.Cleanup.DanglingImages, c.DanglingImages)
			set(&p.Schedule.Cleanup.StoppedContainers, c.StoppedContainers)
		}
		if up := u.Updates; up != nil {
			set(&p.Schedule.Updates.Enabled, up.Enabled)
			set(&p.Schedule.Updates.EveryHours, up.EveryHours)
		}
		if b := u.Backups; b != nil {
			set(&p.Backups.Keep, b.Keep)
			set(&p.Backups.Dir, b.Dir)
		}
		return nil
	})
	if err != nil {
		return Settings{}, err //nolint:wrapcheck // already descriptive
	}
	s.log.Info("settings updated")
	s.scheduler.poke()
	return s.Settings(), nil
}

func (s *Service) SetAutostart(enabled bool) (Settings, error) {
	if !enabled {
		if err := autostart.Disable(); err != nil {
			return Settings{}, fmt.Errorf("disable autostart: %w", err)
		}
		s.log.Info("autostart disabled")
		return s.Settings(), nil
	}
	spec, reason := s.autostartSpec()
	if reason != "" {
		return Settings{}, &userError{msg: reason}
	}
	if err := autostart.Enable(spec); err != nil {
		return Settings{}, fmt.Errorf("enable autostart: %w", err)
	}
	s.log.Info("autostart enabled", "executable", spec.Executable)
	return s.Settings(), nil
}

type userError struct {
	msg string
}

func (e *userError) Error() string {
	return e.msg
}
