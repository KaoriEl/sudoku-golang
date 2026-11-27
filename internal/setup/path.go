package setup

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// InPath reports whether dir is on the current PATH.
func InPath(dir string) bool {
	want := filepath.Clean(dir)
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == "" {
			continue
		}
		if runtime.GOOS == goosWindows || runtime.GOOS == "darwin" {
			if strings.EqualFold(filepath.Clean(p), want) {
				return true
			}
			continue
		}
		if filepath.Clean(p) == want {
			return true
		}
	}
	return false
}
