package configs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveHome(t *testing.T) {
	t.Setenv(HomeEnv, "")
	wd := t.TempDir()
	t.Chdir(wd)
	if err := os.WriteFile(filepath.Join(wd, EnvFileName), []byte("APP_KEY=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveHome(); got != DefaultHome() {
		t.Fatalf("a project's own .env must not become the home: %s", got)
	}
	if err := os.WriteFile(filepath.Join(wd, EnvFileName), []byte("ROOT_PROJECTS_FOLDER=/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveHome(); got != ExpandPath(wd, "") {
		t.Fatalf("sudoku .env in cwd: %s", got)
	}
	t.Setenv(HomeEnv, "~/custom")
	if h, _ := os.UserHomeDir(); ResolveHome() != filepath.Join(h, "custom") {
		t.Fatalf("SUDOKU_HOME: %s", ResolveHome())
	}
}

func TestDataDirFor(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	if DataDirFor(home, root) != home {
		t.Fatal("fresh install: home")
	}
	for _, p := range []string{filepath.Join(root, "compose.yaml"), filepath.Join(root, "sudoku-config", "x")} {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, nil, 0o600)
	}
	if DataDirFor(home, root) != root {
		t.Fatal("old layout: projects folder")
	}
	_ = os.WriteFile(filepath.Join(home, "compose.yaml"), nil, 0o600)
	if DataDirFor(home, root) != home {
		t.Fatal("home wins once it has the infra")
	}
}

func TestExpandPath(t *testing.T) {
	h, _ := os.UserHomeDir()
	base := t.TempDir()
	for in, want := range map[string]string{
		"~/p":                               h + "/p",
		"Users/me/p":                        filepath.Join(base, "Users/me/p"),
		filepath.Join(base, "a", "..", "b"): filepath.Join(base, "b"),
	} {
		if got := ExpandPath(in, base); got != filepath.FromSlash(want) {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
