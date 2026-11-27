//go:build windows

package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sudoku-golang/internal/sysproc"
	"time"
)

const userPathScript = `$p = [Environment]::GetEnvironmentVariable('Path', 'User'); ` +
	`if (-not (($p -split ';') -contains $env:SUDOKU_BIN_DIR)) { ` +
	`[Environment]::SetEnvironmentVariable('Path', (($p.TrimEnd(';') + ';' + $env:SUDOKU_BIN_DIR).TrimStart(';')), 'User') }`

// PathHint describes where AddToPath writes on Windows (the user PATH in the registry).
func PathHint(dir string) (string, string) {
	return "PATH пользователя (реестр)", `[Environment]::SetEnvironmentVariable('Path', $env:Path + ';` + dir + `', 'User')`
}

// AddToPath appends dir to the user PATH via PowerShell (it broadcasts the change to new terminals).
// The dir travels in an environment variable, never inside the script text.
func AddToPath(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", userPathScript)
	cmd.Env = append(os.Environ(), "SUDOKU_BIN_DIR="+dir)
	sysproc.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("powershell: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return "PATH пользователя", nil
}
