package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sudoku-golang/internal/sysproc"
)

const (
	composeProjectLabel = "com.docker.compose.project"
	flagFile            = "--file"
)

var ErrEmptyCommand = errors.New("empty command path")

type Runner struct {
	docker  []string
	compose []string
	env     []string
}

type CommandError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("%s: %s", strings.Join(e.Args, " "), msg)
}

func (e *CommandError) Unwrap() error {
	return e.Err
}

func NewRunner(dockerPath, composePath string, env []string) *Runner {
	return &Runner{
		docker:  SplitCommand(dockerPath),
		compose: SplitCommand(composePath),
		env:     env,
	}
}

// SplitCommand turns DOCKER_PATH / DOCKER_COMPOSE_PATH into argv. A value that is an existing file
// is one argument even with spaces (C:\Program Files\Docker\docker.exe); otherwise it is split on
// whitespace with "double quotes" grouping (`"C:\Program Files\x\docker.exe" compose`).
func SplitCommand(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if st, err := os.Stat(s); err == nil && !st.IsDir() {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	inQuote, has := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote, has = !inQuote, true
		case (r == ' ' || r == '\t') && !inQuote:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

func (r *Runner) exec(ctx context.Context, base []string, args ...string) (string, string, error) {
	if len(base) == 0 {
		return "", "", ErrEmptyCommand
	}
	argv := make([]string, 0, len(base)-1+len(args))
	argv = append(argv, base[1:]...)
	argv = append(argv, args...)

	cmd := exec.CommandContext(ctx, base[0], argv...) //nolint:gosec // binary path comes from operator config, args are not shell-interpreted
	cmd.Env = r.env
	sysproc.HideWindow(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		full := append(append([]string{}, base...), args...)
		return stdout.String(), stderr.String(), &CommandError{Args: full, Stderr: stderr.String(), Err: err}
	}

	return stdout.String(), stderr.String(), nil
}

func (r *Runner) ListComposeContainers(ctx context.Context) ([]Container, error) {
	ids, _, err := r.exec(ctx, r.docker, "ps", "-aq", "--no-trunc", "--filter", "label="+composeProjectLabel)
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(ids)
	if len(fields) == 0 {
		return []Container{}, nil
	}

	return r.Inspect(ctx, fields...)
}

func (r *Runner) Inspect(ctx context.Context, ids ...string) ([]Container, error) {
	args := append([]string{"inspect", "--type", "container"}, ids...)
	out, _, err := r.exec(ctx, r.docker, args...)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}

	var containers []Container
	if jsonErr := json.Unmarshal([]byte(out), &containers); jsonErr != nil {
		return nil, fmt.Errorf("decode docker inspect: %w", jsonErr)
	}

	return containers, nil
}

func (r *Runner) ContainerAction(ctx context.Context, id, action string) error {
	_, _, err := r.exec(ctx, r.docker, action, id)
	return err
}

func (r *Runner) Compose(ctx context.Context, file string, args ...string) (string, error) {
	full := append([]string{flagFile, file}, args...)
	stdout, stderr, err := r.exec(ctx, r.compose, full...)
	return strings.TrimSpace(stdout + stderr), err
}

func (r *Runner) DockerArgv() []string {
	return append([]string{}, r.docker...)
}

func (r *Runner) ComposeArgv() []string {
	return append([]string{}, r.compose...)
}

func (r *Runner) Env() []string {
	return r.env
}

func (r *Runner) Command(ctx context.Context, base []string, args ...string) (*exec.Cmd, error) {
	if len(base) == 0 {
		return nil, ErrEmptyCommand
	}
	argv := make([]string, 0, len(base)-1+len(args))
	argv = append(argv, base[1:]...)
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, base[0], argv...) //nolint:gosec // binary path comes from operator config, args are not shell-interpreted
	cmd.Env = r.env
	sysproc.HideWindow(cmd)
	return cmd, nil
}

func (r *Runner) ComposeConfigJSON(ctx context.Context, file string) ([]byte, error) {
	out, _, err := r.exec(ctx, r.compose, flagFile, file, "config", "--format", "json")
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// ComposeOutput runs a compose command and returns stdout and stderr separately (stderr carries
// compose's warnings, e.g. unset variables).
func (r *Runner) ComposeOutput(ctx context.Context, file string, args ...string) (string, string, error) {
	return r.exec(ctx, r.compose, append([]string{flagFile, file}, args...)...)
}
