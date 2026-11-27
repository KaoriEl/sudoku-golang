package setup

import (
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
	root := filepath.Join(t.TempDir(), "projects")
	wd, _ := os.Getwd()
	t.Chdir(t.TempDir()) // no .env and no sudoku-config here: embedded defaults are used
	defer func() { _ = os.Chdir(wd) }()

	steps, err := Run(Options{Root: root, Prefix: "dev", SkipBinary: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 {
		t.Fatalf("steps: %+v", steps)
	}
	compose, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
	if err != nil || !strings.Contains(string(compose), "container_name: dev_traefik") {
		t.Fatalf("compose: %v\n%s", err, compose)
	}
	env, _ := os.ReadFile(filepath.Join(root, ".env"))
	if !strings.Contains(string(env), "ROOT_PROJECTS_FOLDER="+root) || !strings.Contains(string(env), `DOCKER_COMPOSE_PATH="docker compose"`) {
		t.Fatalf("env:\n%s", env)
	}
	if _, err := os.Stat(filepath.Join(root, "sudoku-config", "traefik", "traefik.yaml")); err != nil {
		t.Fatal(err)
	}
	again, _ := Run(Options{Root: root, SkipBinary: true})
	for _, s := range again {
		if s.Done {
			t.Fatalf("second run must not overwrite anything: %+v", again)
		}
	}
}
