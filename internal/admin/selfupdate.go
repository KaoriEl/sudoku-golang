package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sudoku-golang/internal/selfupdate"
	"sudoku-golang/internal/version"
	"sync"
	"time"
)

// Self-update from the admin: the same release check and verified download as `sudoku update`
// (archive for this OS/arch, SHA-256 from checksums.txt), then the UI offers to restart the admin,
// which re-executes the swapped binary.

const (
	selfUpdateTimeout = 10 * time.Minute
	maxNotes          = 4000
)

var (
	errNoUpdate = errors.New("обновление не требуется: установлена последняя версия")
	errUpdating = errors.New("обновление уже идёт")
)

type selfUpdater interface {
	Latest(ctx context.Context) (selfupdate.Release, error)
	Apply(ctx context.Context, r selfupdate.Release, exe string) error
}

type SelfRelease struct {
	Tag       string    `json:"tag"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Notes     string    `json:"notes"`
	Published time.Time `json:"published"`
	HasAsset  bool      `json:"hasAsset"`
}

type SelfUpdateView struct {
	Current   string       `json:"current"`
	IsRelease bool         `json:"isRelease"`
	Latest    *SelfRelease `json:"latest,omitempty"`
	Newer     bool         `json:"newer"`
	CheckedAt *time.Time   `json:"checkedAt,omitempty"`
	CheckErr  string       `json:"checkError,omitempty"`
	CanApply  bool         `json:"canApply"`
	Reason    string       `json:"reason,omitempty"`
	Exe       string       `json:"exe"`
	// Installed is the version written to disk by an apply; the running process is still Current
	// until the admin restarts.
	Installed string `json:"installed,omitempty"`
}

type selfUpdateState struct {
	mu        sync.Mutex
	latest    *selfupdate.Release
	checkedAt time.Time
	checkErr  string
	installed string
	applying  bool
}

func (s *Service) updater() selfUpdater {
	if s.selfUpd == nil {
		s.selfUpd = selfupdate.New(version.Repo)
	}
	return s.selfUpd
}

func currentExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// applyBlocker explains why this binary cannot replace itself ("" when it can).
func applyBlocker(exe string) string {
	switch {
	case exe == "":
		return "не удалось определить путь к бинарнику"
	case strings.Contains(exe, string(filepath.Separator)+"go-build"):
		return "админка запущена через go run — обновлять нечего, соберите бинарник"
	}
	dir := filepath.Dir(exe)
	f, err := os.CreateTemp(dir, ".sudoku-write-check-*")
	if err != nil {
		return "нет прав на запись в " + dir + " — обновите из терминала: sudoku update"
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return ""
}

func (s *Service) SelfUpdate() SelfUpdateView {
	st := &s.selfUpdate
	st.mu.Lock()
	defer st.mu.Unlock()
	cur := version.Get().Version
	exe, _ := s.exePath()
	view := SelfUpdateView{Current: cur, IsRelease: version.IsRelease(), Exe: exe, CheckErr: st.checkErr, Installed: st.installed}
	if !st.checkedAt.IsZero() {
		at := st.checkedAt
		view.CheckedAt = &at
	}
	if st.latest != nil {
		r := st.latest
		_, hasAsset := r.AssetFor(runtime.GOOS, runtime.GOARCH)
		notes := strings.TrimSpace(r.Body)
		if len(notes) > maxNotes {
			notes = notes[:maxNotes] + "…"
		}
		view.Latest = &SelfRelease{Tag: r.Tag, Name: r.Name, URL: r.URL, Notes: notes, Published: r.Published, HasAsset: hasAsset}
		view.Newer = selfupdate.Newer(cur, r.Tag)
		if st.installed != "" {
			view.Newer = selfupdate.Newer(st.installed, r.Tag)
		}
	}
	view.Reason = applyBlocker(exe)
	if view.Reason == "" && view.Latest != nil && !view.Latest.HasAsset {
		view.Reason = fmt.Sprintf("в релизе %s нет архива для %s/%s", view.Latest.Tag, runtime.GOOS, runtime.GOARCH)
	}
	view.CanApply = view.Reason == "" && view.Latest != nil
	return view
}

func (s *Service) exePath() (string, error) {
	if s.exe != nil {
		return s.exe()
	}
	return currentExe()
}

// CheckSelfUpdate asks GitHub for the latest release.
func (s *Service) CheckSelfUpdate(ctx context.Context) SelfUpdateView {
	r, err := s.updater().Latest(ctx)
	st := &s.selfUpdate
	st.mu.Lock()
	st.checkedAt = time.Now().UTC()
	if err != nil {
		st.checkErr = err.Error()
	} else {
		st.checkErr = ""
		st.latest = &r
	}
	st.mu.Unlock()
	if err != nil {
		s.log.Warn("self-update check failed", "error", err)
	}
	return s.SelfUpdate()
}

// ApplySelfUpdate installs the latest release over the running binary. force installs it even when
// it is not newer (dev builds, reinstall).
func (s *Service) ApplySelfUpdate(ctx context.Context, force bool) (SelfUpdateView, error) {
	view := s.CheckSelfUpdate(ctx)
	if view.Latest == nil {
		return view, &userError{msg: "не удалось получить последний релиз: " + view.CheckErr}
	}
	if !view.CanApply {
		return view, &userError{msg: view.Reason}
	}
	if !view.Newer && !force {
		return view, &userError{msg: errNoUpdate.Error()}
	}
	st := &s.selfUpdate
	st.mu.Lock()
	if st.applying {
		st.mu.Unlock()
		return view, errUpdating
	}
	st.applying = true
	rel := *st.latest
	st.mu.Unlock()
	defer func() {
		st.mu.Lock()
		st.applying = false
		st.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, selfUpdateTimeout)
	defer cancel()
	s.log.Info("self-update", "from", view.Current, "to", rel.Tag, "exe", view.Exe)
	if err := s.updater().Apply(ctx, rel, view.Exe); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return view, &userError{msg: "нет прав на запись " + view.Exe + " — обновите из терминала: sudoku update"}
		}
		return view, fmt.Errorf("обновление до %s: %w", rel.Tag, err)
	}
	st.mu.Lock()
	st.installed = rel.Tag
	st.mu.Unlock()
	s.log.Info("self-update installed", "version", rel.Tag)
	return s.SelfUpdate(), nil
}
