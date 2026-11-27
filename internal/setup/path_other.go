//go:build !windows

package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PathHint is the file AddToPath would change and the line it adds ("" file: unknown shell).
func PathHint(dir string) (string, string) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", pathLine(dir, "")
	}
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh":
		return filepath.Join(h, ".zshrc"), pathLine(dir, "zsh")
	case "bash":
		if runtime.GOOS == "darwin" {
			return filepath.Join(h, ".bash_profile"), pathLine(dir, "bash")
		}
		return filepath.Join(h, ".bashrc"), pathLine(dir, "bash")
	case "fish":
		return filepath.Join(h, ".config", "fish", "conf.d", "sudoku.fish"), pathLine(dir, "fish")
	}
	return "", pathLine(dir, "")
}

// AddToPath appends the PATH line to the shell's rc file (once) and returns the file.
func AddToPath(dir string) (string, error) {
	file, line := PathHint(dir)
	if file == "" {
		return "", fmt.Errorf("неизвестный shell %q — добавьте в PATH вручную: %s", os.Getenv("SHELL"), line)
	}
	raw, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return file, fmt.Errorf("read %s: %w", file, err)
	}
	if strings.Contains(string(raw), line) {
		return file, nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return file, fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return file, fmt.Errorf("open %s: %w", file, err)
	}
	prefix := "\n"
	if len(raw) == 0 || strings.HasSuffix(string(raw), "\n") {
		prefix = ""
	}
	_, err = fmt.Fprintf(f, "%s\n# sudoku\n%s\n", prefix, line)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return file, fmt.Errorf("write %s: %w", file, err)
	}
	return file, nil
}

// pathLine is what goes into a shell rc file; $HOME keeps it portable when the dir is under home.
func pathLine(dir, shell string) string {
	shown := dir
	if h, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(h, dir); err == nil && !strings.HasPrefix(rel, "..") {
			shown = "$HOME/" + filepath.ToSlash(rel)
		}
	}
	if shell == "fish" {
		return "fish_add_path " + shown
	}
	return `export PATH="` + shown + `:$PATH"`
}
