package admin

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sudoku-golang/internal/selfupdate"
	"sudoku-golang/internal/version"
	"testing"
)

type fakeUpdater struct {
	rel     selfupdate.Release
	err     error
	applied string
}

func (f *fakeUpdater) Latest(context.Context) (selfupdate.Release, error) {
	return f.rel, f.err
}

func (f *fakeUpdater) Apply(_ context.Context, r selfupdate.Release, exe string) error {
	f.applied = r.Tag
	return os.WriteFile(exe, []byte(r.Tag), 0o600) //nolint:wrapcheck // test fake
}

func TestSelfUpdate(t *testing.T) {
	old := version.Version
	version.Version = "v1.0.1"
	t.Cleanup(func() { version.Version = old })

	exe := filepath.Join(t.TempDir(), "sudoku")
	if err := os.WriteFile(exe, []byte("v1.0.1"), 0o600); err != nil {
		t.Fatal(err)
	}
	asset := selfupdate.Asset{Name: "sudoku_1.0.2_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"}
	fake := &fakeUpdater{rel: selfupdate.Release{Tag: "v1.0.2", Body: "fixes", Assets: []selfupdate.Asset{asset}}}
	s := &Service{log: slog.New(slog.DiscardHandler), selfUpd: fake, exe: func() (string, error) { return exe, nil }}

	if v := s.SelfUpdate(); v.Latest != nil || v.CanApply || v.Current != "v1.0.1" {
		t.Fatalf("before any check: %+v", v)
	}
	v := s.CheckSelfUpdate(context.Background())
	if !v.Newer || !v.CanApply || v.Latest.Notes != "fixes" || v.CheckedAt == nil {
		t.Fatalf("check: %+v", v)
	}
	v, err := s.ApplySelfUpdate(context.Background(), false)
	if err != nil || v.Installed != "v1.0.2" || v.Newer || fake.applied != "v1.0.2" {
		t.Fatalf("apply: %+v %v", v, err)
	}
	if raw, _ := os.ReadFile(exe); string(raw) != "v1.0.2" {
		t.Fatal("binary not replaced")
	}
	var ue *userError
	if _, err := s.ApplySelfUpdate(context.Background(), false); !errors.As(err, &ue) {
		t.Fatalf("second apply without force must be refused: %v", err)
	}

	fake.rel.Assets = nil
	if v := s.CheckSelfUpdate(context.Background()); v.CanApply || v.Reason == "" {
		t.Fatalf("no asset for this platform: %+v", v)
	}
	fake.err = errors.New("rate limited")
	if v := s.CheckSelfUpdate(context.Background()); v.CheckErr == "" {
		t.Fatalf("check error must be reported: %+v", v)
	}
}
