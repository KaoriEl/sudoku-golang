//go:build !windows

package admin

import (
	"context"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// ptyTerminal is the fallback when the engine API is unreachable: `docker exec -it` under a host pty.
type ptyTerminal struct {
	*os.File
	cmd *exec.Cmd
}

func (t *ptyTerminal) Resize(cols, rows uint16) {
	_ = pty.Setsize(t.File, &pty.Winsize{Cols: cols, Rows: rows})
}

func (t *ptyTerminal) Wait() string {
	if err := t.cmd.Wait(); err != nil {
		return err.Error()
	}
	return "exited"
}

func (s *Service) openPtyTerminal(ctx context.Context, containerID string, cols, rows uint16) (terminal, error) {
	cmd, err := s.docker.Command(ctx, s.docker.DockerArgv(), "exec", "-it", "-e", "TERM=xterm-256color", containerID, "sh", "-c", shellScript)
	if err != nil {
		return nil, err //nolint:wrapcheck // ErrEmptyCommand
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err //nolint:wrapcheck // descriptive
	}
	return &ptyTerminal{File: f, cmd: cmd}, nil
}
