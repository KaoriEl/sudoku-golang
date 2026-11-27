package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/sysproc"
	"time"
)

const probeTimeout = 10 * time.Second

var (
	errEmptyCommand = errors.New("пустая команда")
	errNotDocker    = errors.New("это не docker")
)

// dockerLocations are where Docker Desktop, OrbStack, Rancher Desktop, Homebrew and Colima put the CLI
// when it is not on PATH (e.g. a fresh login shell or a GUI-launched terminal).
func dockerLocations() []string {
	h, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case goosWindows:
		pf := os.Getenv("ProgramFiles")
		if pf == "" {
			pf = `C:\Program Files`
		}
		return []string{
			filepath.Join(pf, "Docker", "Docker", "resources", "bin", "docker.exe"),
			filepath.Join(pf, "Rancher Desktop", "resources", "resources", "win32", "bin", "docker.exe"),
		}
	case goosDarwin:
		return []string{
			"/usr/local/bin/docker",
			"/opt/homebrew/bin/docker",
			"/Applications/Docker.app/Contents/Resources/bin/docker",
			filepath.Join(h, ".orbstack", "bin", "docker"),
			filepath.Join(h, ".rd", "bin", "docker"),
		}
	}
	return []string{"/usr/bin/docker", "/usr/local/bin/docker", "/snap/bin/docker", filepath.Join(h, ".rd", "bin", "docker")}
}

func composeLocations() []string {
	switch runtime.GOOS {
	case goosWindows:
		return nil
	case goosDarwin:
		return []string{"/usr/local/bin/docker-compose", "/opt/homebrew/bin/docker-compose"}
	}
	return []string{"/usr/bin/docker-compose", "/usr/local/bin/docker-compose"}
}

// probe runs argv + args and returns the first output line.
func probe(argv []string, args ...string) (string, error) {
	if len(argv) == 0 {
		return "", errEmptyCommand
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	full := append(append([]string{}, argv[1:]...), args...)
	cmd := exec.CommandContext(ctx, argv[0], full...) //nolint:gosec // the docker/compose command the user chose, argv without a shell
	sysproc.HideWindow(cmd)
	out, err := cmd.Output()
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if err != nil {
		return "", fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}
	return strings.TrimSpace(line), nil
}

// CheckDocker validates a DOCKER_PATH value and returns the client version. `docker --version` needs no
// daemon, so a stopped Docker Desktop / Colima does not hide a working CLI.
func CheckDocker(cmd string) (string, error) {
	line, err := probe(docker.SplitCommand(cmd), "--version")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(strings.ToLower(line), "docker version") {
		return "", fmt.Errorf("%w: %q", errNotDocker, line)
	}
	v := strings.TrimSpace(strings.TrimPrefix(line[len("docker version"):], " "))
	v, _, _ = strings.Cut(v, ",")
	return v, nil
}

// CheckCompose validates a DOCKER_COMPOSE_PATH value and returns the compose version.
func CheckCompose(cmd string) (string, error) {
	return probe(docker.SplitCommand(cmd), "version", "--short")
}

// DetectDocker finds a working docker CLI: PATH first, then the usual install locations. The value is an
// absolute path, so autostart and GUI-launched shells with a short PATH still find it.
func DetectDocker() (string, string, bool) {
	candidates := []string{}
	if p, err := exec.LookPath("docker"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		candidates = append(candidates, p)
	}
	candidates = append(candidates, dockerLocations()...)
	for _, c := range candidates {
		if !exists(c) {
			continue
		}
		if v, err := CheckDocker(c); err == nil {
			return c, v, true
		}
	}
	return "", "", false
}

// ComposeFor is the compose-plugin command for a docker CLI ("<docker> compose", quoted when needed).
func ComposeFor(dockerCmd string) string {
	if strings.ContainsAny(dockerCmd, " \t") && !strings.HasPrefix(dockerCmd, `"`) && exists(dockerCmd) {
		return `"` + dockerCmd + `" compose`
	}
	return dockerCmd + " compose"
}

// DetectCompose prefers the compose plugin of the given docker CLI, then a standalone docker-compose.
func DetectCompose(dockerCmd string) (string, string, bool) {
	if dockerCmd != "" {
		c := ComposeFor(dockerCmd)
		if v, err := CheckCompose(c); err == nil {
			return c, v, true
		}
	}
	candidates := []string{}
	if p, err := exec.LookPath("docker-compose"); err == nil {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, composeLocations()...)
	for _, c := range candidates {
		if !exists(c) {
			continue
		}
		if v, err := CheckCompose(c); err == nil {
			return c, v, true
		}
	}
	return "", "", false
}
