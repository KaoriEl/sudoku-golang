// Package version carries build metadata. Release builds set the variables with
// -ldflags "-X sudoku-golang/internal/version.Version=v1.2.3 ..."; local builds fall back to the VCS
// info Go embeds.
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
	// Repo is the GitHub owner/name `sudoku update` downloads releases from.
	Repo = "KaoriEl/sudoku-golang"
)

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Dirty   bool   `json:"dirty"`
}

func Get() Info {
	info := Info{Version: Version, Commit: Commit, Date: Date, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			case "vcs.modified":
				info.Dirty = s.Value == "true"
			}
		}
	}
	if len(info.Commit) > 12 {
		info.Commit = info.Commit[:12]
	}
	return info
}

// IsRelease reports a tagged build (vX.Y.Z), which is what self-update can compare against.
func IsRelease() bool {
	return strings.HasPrefix(Version, "v")
}

func (i Info) String() string {
	s := i.Version
	if i.Commit != "" {
		s += " (" + i.Commit
		if i.Dirty {
			s += "-dirty"
		}
		s += ")"
	}
	return s + " " + i.OS + "/" + i.Arch
}
