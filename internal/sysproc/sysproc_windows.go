//go:build windows

package sysproc

import (
	"os/exec"
	"syscall"
)

// HideWindow keeps a child process from flashing a console window when the server runs without one
// (autostart from the Startup folder).
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}
