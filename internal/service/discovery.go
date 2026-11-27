package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sudoku-golang/internal/infra/configs"
)

var ErrRootNotSet = errors.New("ROOT_PROJECTS_FOLDER env is not set")

const projectGlob = "*/compose-sudoku.yaml"

// Layout is where sudoku finds things: the infra compose.yaml lives in Data (the sudoku home, or
// the projects folder in old installs), project compose files in Root/<project>/compose-sudoku.yaml.
type Layout struct {
	Data string
	Root string
}

func LayoutOf(cfg *configs.Config) Layout {
	return Layout{Data: abs(cfg.DataDir), Root: abs(cfg.RootProjectsFolder)}
}

func abs(p string) string {
	if p == "" {
		return ""
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// InfraFile is the infrastructure compose file.
func (l Layout) InfraFile() string {
	return filepath.Join(l.Data, configs.InfraComposeName)
}

// IsInfra reports whether file is the infrastructure compose file.
func (l Layout) IsInfra(file string) bool {
	return filepath.Clean(file) == l.InfraFile()
}

// Rel names a compose file for keys and messages: "compose.yaml" for infra, else relative to Root.
func (l Layout) Rel(file string) string {
	if l.IsInfra(file) {
		return configs.InfraComposeName
	}
	rel, err := filepath.Rel(l.Root, file)
	if err != nil || l.Root == "" {
		return filepath.ToSlash(file)
	}
	return filepath.ToSlash(rel)
}

// ConfigDir is sudoku-config/ inside the data directory.
func (l Layout) ConfigDir() string {
	return filepath.Join(l.Data, configs.ConfigDirName)
}

// Discover is the single source of compose-file discovery: the infra compose.yaml first, then every
// Root/*/compose-sudoku.yaml (paths containing "_noscan" are skipped).
func (l Layout) Discover() ([]string, error) {
	var files []string
	if l.Data != "" {
		if _, err := os.Stat(l.InfraFile()); err == nil {
			files = append(files, l.InfraFile())
		}
	}
	if l.Root == "" {
		if len(files) == 0 {
			return nil, ErrRootNotSet
		}
		return files, nil
	}
	matches, err := filepath.Glob(filepath.Join(l.Root, projectGlob))
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", projectGlob, err)
	}
	for _, m := range matches {
		if strings.Contains(m, "_noscan") {
			continue
		}
		files = append(files, abs(m))
	}
	return files, nil
}

// DiscoverComposeFiles is Layout.Discover for a loaded config.
func DiscoverComposeFiles(cfg *configs.Config) ([]string, error) {
	return LayoutOf(cfg).Discover()
}
