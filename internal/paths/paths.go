// Package paths normalises host paths so compose files can be compared with the paths Docker stores
// in container labels, on every OS.
package paths

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const maxCached = 512

var resolved struct {
	sync.Mutex
	m map[string]string
}

// Normalize cleans p, resolves symlinks (cached: it is called for every container on every poll and
// each call lstat()s every component) and, on Windows and macOS, folds case because their default
// file systems are case-insensitive while Docker keeps whatever spelling compose used.
func Normalize(p string) string {
	p = filepath.Clean(p)
	resolved.Lock()
	if r, ok := resolved.m[p]; ok {
		resolved.Unlock()
		return r
	}
	resolved.Unlock()

	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return fold(p) // a missing file is not cached: it may appear later
	}
	r = fold(r)

	resolved.Lock()
	if resolved.m == nil || len(resolved.m) >= maxCached {
		resolved.m = map[string]string{}
	}
	resolved.m[p] = r
	resolved.Unlock()
	return r
}

func fold(p string) string {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(p)
	}
	return p
}

// Same reports whether a and b point to the same file.
func Same(a, b string) bool {
	return Normalize(a) == Normalize(b)
}
