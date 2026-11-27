//go:build !windows

package commands

import (
	"fmt"
	"syscall"
)

// reexec replaces the process with a fresh `sudoku serve` that re-reads .env (same PID, so launchd /
// systemd keep tracking it). Variables that came from .env are dropped from the environment,
// otherwise godotenv would keep the old values.
func reexec(dropAddrFlag bool) error {
	argv, env, err := reexecArgs(dropAddrFlag)
	if err != nil {
		return err
	}
	if err := syscall.Exec(argv[0], argv, env); err != nil { //nolint:gosec // re-executes this very binary
		return fmt.Errorf("exec %s: %w", argv[0], err)
	}
	return nil
}
