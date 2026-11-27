package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var ErrRootNotSet = errors.New("ROOT_PROJECTS_FOLDER env is not set")

var composeGlobPatterns = []string{"compose.yaml", "*/compose-sudoku.yaml"}

func DiscoverComposeFiles(root string) ([]string, error) {
	if root == "" {
		return nil, ErrRootNotSet
	}

	var files []string
	for _, pattern := range composeGlobPatterns {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", pattern, err)
		}
		for _, match := range matches {
			if strings.Contains(match, "_noscan") {
				continue
			}
			absPath, err := filepath.Abs(match)
			if err != nil {
				return nil, fmt.Errorf("abs path %q: %w", match, err)
			}
			files = append(files, absPath)
		}
	}

	return files, nil
}
