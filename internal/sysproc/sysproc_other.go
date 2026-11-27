//go:build !windows

// Package sysproc holds the few process tweaks that differ between operating systems.
package sysproc

import "os/exec"

// HideWindow is a no-op outside Windows.
func HideWindow(*exec.Cmd) {}
