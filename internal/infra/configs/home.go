package configs

import (
	"os"
	"path/filepath"
	"strings"
)

// HomeEnv overrides the sudoku home directory (where .env, the infra compose.yaml and sudoku-config/ live).
const HomeEnv = "SUDOKU_HOME"

// InfraComposeName is the infrastructure compose file inside the data directory.
const InfraComposeName = "compose.yaml"

// ConfigDirName is the folder with infra configs and sudoku state (admin.yaml, sets, runs…).
const ConfigDirName = "sudoku-config"

const homeDirName = ".sudoku"

// home is where .env was looked up by the last Load; EnvPath depends on it.
var home string

// DefaultHome is ~/.sudoku (%USERPROFILE%\.sudoku on Windows).
func DefaultHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return homeDirName
	}
	return filepath.Join(h, homeDirName)
}

// ExpandPath expands a leading ~ and makes p absolute against base (when relative).
func ExpandPath(p, base string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[1:])
		}
	}
	if !filepath.IsAbs(p) && base != "" {
		p = filepath.Join(base, p)
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

// IsSudokuEnv reports whether dir/.env is a sudoku config (has ROOT_PROJECTS_FOLDER), not some
// project's own .env.
func IsSudokuEnv(dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, EnvFileName))
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(raw), "\n") {
		l = strings.TrimPrefix(strings.TrimSpace(l), "export ")
		if k, _, ok := strings.Cut(l, "="); ok && strings.TrimSpace(k) == "ROOT_PROJECTS_FOLDER" {
			return true
		}
	}
	return false
}

// ResolveHome picks the directory whose .env is read: $SUDOKU_HOME, else the working directory when
// it holds a sudoku .env (the old layout and a repository checkout), else ~/.sudoku.
func ResolveHome() string {
	if h := os.Getenv(HomeEnv); h != "" {
		return ExpandPath(h, "")
	}
	if wd, err := os.Getwd(); err == nil && IsSudokuEnv(wd) {
		return wd
	}
	return DefaultHome()
}

// Home is the directory the last Load read .env from.
func Home() string {
	if home == "" {
		return ResolveHome()
	}
	return home
}

// EnvPath is the .env sudoku reads and the admin edits.
func EnvPath() string {
	return filepath.Join(Home(), EnvFileName)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// DataDirFor decides where the infra compose.yaml, sudoku-config/ and backups live: the home, unless
// only the projects folder has them (installs made before the home directory existed).
func DataDirFor(homeDir, root string) string {
	if exists(filepath.Join(homeDir, InfraComposeName)) {
		return homeDir
	}
	if root != "" && exists(filepath.Join(root, InfraComposeName)) && exists(filepath.Join(root, ConfigDirName)) {
		return root
	}
	return homeDir
}

// InfraCompose is the infrastructure compose file (it may not exist yet).
func (c *Config) InfraCompose() string {
	return filepath.Join(c.DataDir, InfraComposeName)
}

// Legacy reports the old layout: infra and state inside the projects folder.
func (c *Config) Legacy() bool {
	return c.DataDir != c.Home && c.DataDir == c.RootProjectsFolder
}
