package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteNames(t *testing.T) {
	in := "services:\n  traefik:\n    container_name: \"${PREFIX_CONTAINER_NAME}_traefik\"\n    hostname: 'traefik'\n"
	out := string(RewriteNames([]byte(in), "work.idea"))
	if !strings.Contains(out, "    container_name: work.idea_traefik\n") || !strings.Contains(out, "    hostname: traefik\n") {
		t.Fatalf("got:\n%s", out)
	}
	if out := string(RewriteNames([]byte(in), "")); !strings.Contains(out, "container_name: traefik") {
		t.Fatalf("empty prefix:\n%s", out)
	}
}

func TestRunFromEmbeddedDefaults(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(t.TempDir(), ".sudoku")
	t.Chdir(t.TempDir()) // no .env and no sudoku-config here: embedded defaults are used

	steps, err := Run(Options{Home: home, Root: root, Prefix: "dev", SkipBinary: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 {
		t.Fatalf("steps: %+v", steps)
	}
	compose, err := os.ReadFile(filepath.Join(home, "compose.yaml"))
	if err != nil || !strings.Contains(string(compose), "container_name: dev_traefik") {
		t.Fatalf("compose: %v\n%s", err, compose)
	}
	env, _ := os.ReadFile(filepath.Join(home, ".env"))
	if !strings.Contains(string(env), "ROOT_PROJECTS_FOLDER="+root) || !strings.Contains(string(env), `DOCKER_COMPOSE_PATH="docker compose"`) {
		t.Fatalf("env:\n%s", env)
	}
	traefik := filepath.Join(home, "sudoku-config", "traefik", "traefik.yaml")
	if err := os.WriteFile(traefik, []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("projects folder must stay untouched: %v", entries)
	}

	again, err := Run(Options{Home: home, Root: root, SkipBinary: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range again {
		if s.Done {
			t.Fatalf("second run must not change anything: %+v", again)
		}
	}
	if raw, _ := os.ReadFile(traefik); string(raw) != "edited\n" {
		t.Fatalf("edited config overwritten: %q", raw)
	}

	other := t.TempDir()
	if _, err := Run(Options{Home: home, Root: other, SkipBinary: true}); err != nil {
		t.Fatal(err)
	}
	env, _ = os.ReadFile(filepath.Join(home, ".env"))
	if !strings.Contains(string(env), "ROOT_PROJECTS_FOLDER="+other) || !strings.Contains(string(env), "PREFIX_CONTAINER_NAME=dev") {
		t.Fatalf("root must be patched in place:\n%s", env)
	}
}

func TestRunRejectsMissingRoot(t *testing.T) {
	_, err := Run(Options{Home: t.TempDir(), Root: filepath.Join(t.TempDir(), "nope"), SkipBinary: true})
	if !errors.Is(err, ErrRootMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestMigrate(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(t.TempDir(), "old")
	home := filepath.Join(t.TempDir(), ".sudoku")
	t.Chdir(t.TempDir())
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(old, ".env"), "ROOT_PROJECTS_FOLDER="+old+"\nPREFIX_CONTAINER_NAME=work\nMAX_WORKERS=10\n")
	write(filepath.Join(old, "compose.yaml"), "services:\n  redis:\n    container_name: work_redis\n")
	write(filepath.Join(old, "sudoku-config", "redis", "data", "dump.rdb"), "DATA")
	write(filepath.Join(old, "backups", "x.sql.gz"), "DUMP")
	write(filepath.Join(old, "sudoku"), "BIN")
	if !IsLegacy(old) {
		t.Fatal("old install not detected")
	}

	if _, err := Run(Options{Home: home, Root: root, MigrateFrom: old, SkipBinary: true}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(home, "sudoku-config", "redis", "data", "dump.rdb")); string(raw) != "DATA" {
		t.Fatal("redis data not moved")
	}
	if raw, _ := os.ReadFile(filepath.Join(home, "compose.yaml")); !strings.Contains(string(raw), "work_redis") {
		t.Fatalf("compose regenerated instead of moved: %s", raw)
	}
	if !exists(filepath.Join(home, "backups", "x.sql.gz")) || !exists(filepath.Join(home, "sudoku-config", "traefik", "traefik.yaml")) {
		t.Fatal("backups not moved or missing defaults not added")
	}
	env, _ := os.ReadFile(filepath.Join(home, ".env"))
	if !strings.Contains(string(env), "ROOT_PROJECTS_FOLDER="+root) || !strings.Contains(string(env), "MAX_WORKERS=10") {
		t.Fatalf("env:\n%s", env)
	}
	known, other := Leftovers(old)
	if len(other) != 0 || len(known) != 1 {
		t.Fatalf("leftovers: %v / %v", known, other)
	}
}

func TestDockerPathsRoundTrip(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Chdir(t.TempDir())
	o := Options{
		Home: home, Root: root, SkipBinary: true,
		DockerPath:  `C:\Program Files\Docker\docker.exe`,
		ComposePath: `"C:\Program Files\Docker\docker.exe" compose`,
	}
	if _, err := Run(o); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(home, ".env")
	if got := EnvValue(env, "DOCKER_PATH"); got != o.DockerPath {
		t.Fatalf("DOCKER_PATH round trip: %q", got)
	}
	if got := EnvValue(env, "DOCKER_COMPOSE_PATH"); got != o.ComposePath {
		t.Fatalf("DOCKER_COMPOSE_PATH round trip: %q", got)
	}
	o.ComposePath = "/usr/local/bin/docker-compose"
	if _, err := Run(o); err != nil {
		t.Fatal(err)
	}
	if got := EnvValue(env, "DOCKER_COMPOSE_PATH"); got != o.ComposePath {
		t.Fatalf("patched compose: %q", got)
	}
}

func TestComposeFor(t *testing.T) {
	if got := ComposeFor("docker"); got != "docker compose" {
		t.Fatal(got)
	}
}

func TestCheckDockerRejectsOtherBinaries(t *testing.T) {
	if _, err := CheckDocker("/bin/echo"); err == nil {
		t.Fatal("echo accepted as docker")
	}
	if _, err := CheckDocker("/definitely/missing/docker"); err == nil {
		t.Fatal("missing binary accepted")
	}
}
