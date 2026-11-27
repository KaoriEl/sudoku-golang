//go:build windows

package commands

import (
	"fmt"
	"os"
	"os/exec"
)

// reexec on Windows (no exec(2)): start a fresh `sudoku serve` that re-reads .env and exit. The new
// process retries binding the port until this one has released it (SUDOKU_REEXEC).
func reexec(dropAddrFlag bool) error {
	argv, env, err := reexecArgs(dropAddrFlag)
	if err != nil {
		return err
	}
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec,noctx // re-executes this very binary, outlives us
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", argv[0], err)
	}
	os.Exit(0)
	return nil
}
