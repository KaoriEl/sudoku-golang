// Package shellquote quotes arguments for copy-paste commands: POSIX sh on Linux/macOS, PowerShell on
// Windows. sudoku itself never runs commands through a shell.
package shellquote

import (
	"regexp"
	"runtime"
	"strings"
)

var safeRe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// Quote quotes s for the shell an operator uses on this OS.
func Quote(s string) string {
	if runtime.GOOS == "windows" {
		return PowerShell(s)
	}
	return POSIX(s)
}

// POSIX returns s unchanged when it contains only shell-safe characters, otherwise wraps it in
// single quotes (embedded single quotes are closed, escaped and reopened).
func POSIX(s string) string {
	if s == "" {
		return "''"
	}
	if safeRe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// PowerShell single-quotes s (a literal string; ' is escaped by doubling it). Backslashes are safe
// unquoted there, so Windows paths without spaces stay readable.
func PowerShell(s string) string {
	if s == "" {
		return "''"
	}
	if safeRe.MatchString(strings.ReplaceAll(s, `\`, "/")) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// Join quotes every argument and joins them with spaces.
func Join(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = Quote(a)
	}
	return strings.Join(quoted, " ")
}
