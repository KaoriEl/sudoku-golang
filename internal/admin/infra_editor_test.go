package admin

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/service"
	"testing"
)

const fakeCompose = `#!/bin/sh
for a in "$@"; do f="$a"; case "$prev" in --file|-f) file="$a";; esac; prev="$a"; done
if grep -q broken "$file"; then echo "services.broken: additional property nope is not allowed" >&2; exit 1; fi
exit 0
`

func editorService(t *testing.T) (*Service, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake compose is a shell script")
	}
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "compose")
	if err := os.WriteFile(bin, []byte(fakeCompose), 0o700); err != nil { //nolint:gosec // test helper must be executable
		t.Fatal(err)
	}
	traefik := filepath.Join(home, "sudoku-config", "traefik", "traefik.yaml")
	if err := os.MkdirAll(filepath.Dir(traefik), 0o755); err != nil {
		t.Fatal(err)
	}
	compose := "services:\n  traefik:\n    image: traefik:2\n    volumes:\n      - ./sudoku-config/traefik/traefik.yaml:/etc/traefik/traefik.yaml:ro\n"
	for p, c := range map[string]string{filepath.Join(home, "compose.yaml"): compose, traefik: "entryPoints:\n  web:\n    address: \":80\"\n"} {
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return &Service{
		log:    slog.New(slog.DiscardHandler),
		layout: service.Layout{Data: home, Root: t.TempDir()},
		docker: docker.NewRunner("docker", bin, os.Environ()),
		hub:    newHub(),
	}, home
}

func TestInfraFiles(t *testing.T) {
	s, home := editorService(t)
	view, err := s.InfraFiles()
	if err != nil || len(view.Files) != 2 {
		t.Fatalf("%+v %v", view, err)
	}
	if view.Files[0].Path != filepath.Join(home, "compose.yaml") || !view.Files[0].Exists {
		t.Fatalf("compose: %+v", view.Files[0])
	}
	if view.Files[1].Path != filepath.Join(home, "sudoku-config", "traefik", "traefik.yaml") {
		t.Fatalf("traefik path follows the compose volume: %+v", view.Files[1])
	}
}

func TestSaveInfraFile(t *testing.T) {
	s, home := editorService(t)
	ctx := context.Background()
	view, _ := s.InfraFiles()
	compose := view.Files[0]

	if _, err := s.SaveInfraFile(ctx, "compose", InfraSaveRequest{Content: compose.Content + "\n", Hash: "stale"}); !errors.Is(err, ErrStaleFile) {
		t.Fatalf("stale hash: %v", err)
	}
	for _, bad := range []string{"services: [", "foo: 1\n", compose.Content + "  broken:\n    image: x\n"} {
		_, err := s.SaveInfraFile(ctx, "compose", InfraSaveRequest{Content: bad, Hash: compose.Hash})
		var ue *userError
		if !errors.As(err, &ue) {
			t.Fatalf("%q must be rejected, got %v", bad, err)
		}
	}
	if raw, _ := os.ReadFile(compose.Path); string(raw) != compose.Content {
		t.Fatal("a rejected save must not touch the file")
	}

	next := compose.Content + "  rabbitmq:\n    image: rabbitmq:3.13-management\n"
	saved, err := s.SaveInfraFile(ctx, "compose", InfraSaveRequest{Content: next, Hash: compose.Hash})
	if err != nil || saved.File.Content != next || saved.File.Hash == compose.Hash || !saved.File.HasBak {
		t.Fatalf("save: %+v %v", saved, err)
	}
	if bak, _ := os.ReadFile(compose.Path + ".sudoku.bak"); string(bak) != compose.Content {
		t.Fatal("backup must hold the previous version")
	}
	if entries, _ := os.ReadDir(home); len(entries) != 3 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("temp files left behind: %s", strings.Join(names, ", "))
	}
	if _, err := s.SaveInfraFile(ctx, "compose", InfraSaveRequest{Content: next, Hash: compose.Hash}); !errors.Is(err, ErrStaleFile) {
		t.Fatalf("second save with the old hash: %v", err)
	}
	if _, err := s.SaveInfraFile(ctx, "../etc", InfraSaveRequest{Content: "a: 1\n"}); !errors.Is(err, ErrUnknownInfraFile) {
		t.Fatalf("unknown id: %v", err)
	}
}
