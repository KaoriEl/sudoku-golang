// Package prefs is ${ROOT}/sudoku-config/admin.yaml: settings shared by the CLI and the admin panel
// (metrics, startup order, notifications, schedules, backups).
package prefs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sudoku-golang/internal/fsutil"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const header = "# Настройки sudoku (CLI и админка). Меняются на странице «Настройки» или руками.\n"

const (
	DefaultWaitTimeout    = 180
	MinWaitTimeout        = 10
	MaxWaitTimeout        = 1800
	DefaultBackupsKeep    = 10
	MaxBackupsKeep        = 1000
	DefaultCleanupHours   = 168
	DefaultUpdatesHours   = 24
	MinScheduleHours      = 1
	MaxScheduleHours      = 24 * 90
	DefaultCleanupCache   = "168h"
	DefaultNotifyCooldown = 60
)

type Metrics struct {
	Background         bool `yaml:"background" json:"background"`
	LiveInterval       int  `yaml:"liveInterval,omitempty" json:"liveInterval"`
	BackgroundInterval int  `yaml:"backgroundInterval,omitempty" json:"backgroundInterval"`
}

// Startup controls the order of `start`/`restart`: infrastructure (root compose.yaml) first, then wait
// until its healthchecks pass, then the projects.
type Startup struct {
	WaitHealthy *bool `yaml:"waitHealthy,omitempty" json:"waitHealthy"`
	WaitTimeout int   `yaml:"waitTimeout,omitempty" json:"waitTimeout"`
}

func (s Startup) Wait() bool {
	return s.WaitHealthy == nil || *s.WaitHealthy
}

func (s Startup) Timeout() time.Duration {
	if s.WaitTimeout <= 0 {
		return DefaultWaitTimeout * time.Second
	}
	return time.Duration(max(MinWaitTimeout, min(MaxWaitTimeout, s.WaitTimeout))) * time.Second
}

// Notifications: Desktop = OS notifications sent by the server (works with no tab open), Browser =
// Web Notifications from an open tab.
type Notifications struct {
	Desktop bool  `yaml:"desktop" json:"desktop"`
	Browser *bool `yaml:"browser,omitempty" json:"browser"`
}

func (n Notifications) BrowserOn() bool {
	return n.Browser == nil || *n.Browser
}

type CleanupSchedule struct {
	Enabled           bool      `yaml:"enabled" json:"enabled"`
	EveryHours        int       `yaml:"everyHours,omitempty" json:"everyHours"`
	BuildCache        string    `yaml:"buildCache,omitempty" json:"buildCache"`
	DanglingImages    bool      `yaml:"danglingImages" json:"danglingImages"`
	StoppedContainers bool      `yaml:"stoppedContainers" json:"stoppedContainers"`
	LastRun           time.Time `yaml:"lastRun,omitempty" json:"lastRun"`
	LastResult        string    `yaml:"lastResult,omitempty" json:"lastResult"`
}

type UpdatesSchedule struct {
	Enabled    bool      `yaml:"enabled" json:"enabled"`
	EveryHours int       `yaml:"everyHours,omitempty" json:"everyHours"`
	LastRun    time.Time `yaml:"lastRun,omitempty" json:"lastRun"`
}

type Schedule struct {
	Cleanup CleanupSchedule `yaml:"cleanup" json:"cleanup"`
	Updates UpdatesSchedule `yaml:"updates" json:"updates"`
}

type Backups struct {
	Keep int    `yaml:"keep,omitempty" json:"keep"`
	Dir  string `yaml:"dir,omitempty" json:"dir"`
}

type Prefs struct {
	Metrics       Metrics       `yaml:"metrics" json:"metrics"`
	Startup       Startup       `yaml:"startup" json:"startup"`
	Notifications Notifications `yaml:"notifications" json:"notifications"`
	Schedule      Schedule      `yaml:"schedule" json:"schedule"`
	Backups       Backups       `yaml:"backups" json:"backups"`
}

// Normalized fills defaults so readers never deal with zero values.
func (p Prefs) Normalized() Prefs {
	if p.Startup.WaitTimeout <= 0 {
		p.Startup.WaitTimeout = DefaultWaitTimeout
	}
	if p.Backups.Keep <= 0 {
		p.Backups.Keep = DefaultBackupsKeep
	}
	if p.Schedule.Cleanup.EveryHours <= 0 {
		p.Schedule.Cleanup.EveryHours = DefaultCleanupHours
	}
	if p.Schedule.Cleanup.BuildCache == "" {
		p.Schedule.Cleanup.BuildCache = DefaultCleanupCache
	}
	if p.Schedule.Updates.EveryHours <= 0 {
		p.Schedule.Updates.EveryHours = DefaultUpdatesHours
	}
	return p
}

func Path(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return filepath.Join(abs, "sudoku-config", "admin.yaml")
}

// BackupDir is where database dumps go: prefs value, or ${ROOT}/backups.
func (p Prefs) BackupDir(root string) string {
	if p.Backups.Dir != "" {
		if filepath.IsAbs(p.Backups.Dir) {
			return p.Backups.Dir
		}
		return filepath.Join(root, p.Backups.Dir)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return filepath.Join(abs, "backups")
}

func Load(path string) (Prefs, error) {
	var p Prefs
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("parse %s: %w", path, err)
	}
	return p, nil
}

func Save(path string, p Prefs) error {
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(p); err != nil {
		return fmt.Errorf("encode prefs: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode prefs: %w", err)
	}
	return fsutil.WriteFileAtomic(path, buf.Bytes()) //nolint:wrapcheck // already descriptive
}

// Store serialises read-modify-write cycles on the file within one process.
type Store struct {
	mu   sync.Mutex
	path string
}

func NewStore(root string) *Store {
	return &Store{path: Path(root)}
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Get() (Prefs, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Load(s.path)
}

// Update loads, applies fn and saves; fn returning an error aborts without writing.
func (s *Store) Update(fn func(*Prefs) error) (Prefs, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := Load(s.path)
	if err != nil {
		return p, err
	}
	if err := fn(&p); err != nil {
		return p, err
	}
	return p, Save(s.path, p)
}
