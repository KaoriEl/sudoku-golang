// Package domains helps the local domains served by Traefik work: it checks how each host resolves,
// maintains a marked block in the OS hosts file, and issues locally trusted TLS certificates with
// mkcert plus the Traefik dynamic config that uses them.
package domains

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	osWindows      = "windows"
	blockBegin     = "# >>> sudoku: локальные домены (управляется sudoku, не редактируйте вручную)"
	blockEnd       = "# <<< sudoku"
	lookupTimeout  = 2 * time.Second
	lookupWorkers  = 8
	loopbackTarget = "127.0.0.1"
)

var ErrNeedsElevation = errors.New("нет прав на запись hosts-файла")

// Host is one domain from the Traefik router rules.
type Host struct {
	Name       string   `json:"name"`
	Addrs      []string `json:"addrs"`
	Resolves   bool     `json:"resolves"`
	Local      bool     `json:"local"`
	AutoLocal  bool     `json:"autoLocal"`
	InBlock    bool     `json:"inBlock"`
	InCert     bool     `json:"inCert"`
	Containers []string `json:"containers"`
}

// HostsFile is the OS hosts file.
func HostsFile() string {
	if runtime.GOOS == osWindows {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return filepath.Join(root, "System32", "drivers", "etc", "hosts")
	}
	return "/etc/hosts"
}

func validHost(h string) bool {
	if h == "" || len(h) > 253 || strings.ContainsAny(h, " \t\r\n#*{}$`") {
		return false
	}
	return net.ParseIP(h) == nil
}

// Lookup resolves every host in parallel (with a short timeout each).
func Lookup(ctx context.Context, names map[string][]string) []Host {
	keys := make([]string, 0, len(names))
	for n := range names {
		if validHost(n) {
			keys = append(keys, n)
		}
	}
	sort.Strings(keys)
	out := make([]Host, len(keys))
	sem := make(chan struct{}, lookupWorkers)
	var wg sync.WaitGroup
	for i, n := range keys {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, n string) {
			defer wg.Done()
			defer func() { <-sem }()
			h := Host{Name: n, Containers: names[n], AutoLocal: strings.HasSuffix(n, ".localhost") || n == "localhost", Addrs: []string{}}
			lctx, cancel := context.WithTimeout(ctx, lookupTimeout)
			addrs, err := net.DefaultResolver.LookupHost(lctx, n)
			cancel()
			if err == nil && len(addrs) > 0 {
				h.Resolves, h.Addrs, h.Local = true, addrs, true
				for _, a := range addrs {
					if ip := net.ParseIP(a); ip == nil || !ip.IsLoopback() {
						h.Local = false
					}
				}
			}
			out[i] = h
		}(i, n)
	}
	wg.Wait()
	return out
}

// ReadBlock returns the hosts listed in sudoku's block of the hosts file content.
func ReadBlock(content string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == blockBegin:
			in = true
		case t == blockEnd:
			in = false
		case in && t != "" && !strings.HasPrefix(t, "#"):
			fields := strings.Fields(t)
			out = append(out, fields[1:]...)
		}
	}
	sort.Strings(out)
	return out
}

// Apply returns content with sudoku's block replaced by one mapping hosts to 127.0.0.1 (and ::1),
// keeping everything else byte for byte. An empty list removes the block.
func Apply(content string, hosts []string) string {
	eol := "\n"
	if strings.Contains(content, "\r\n") || (content == "" && runtime.GOOS == osWindows) {
		eol = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var kept []string
	in := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case t == blockBegin:
			in = true
		case t == blockEnd:
			in = false
		case !in:
			kept = append(kept, line)
		}
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	if len(hosts) > 0 {
		sorted := append([]string(nil), hosts...)
		sort.Strings(sorted)
		kept = append(kept, "", blockBegin)
		for _, h := range sorted {
			kept = append(kept, loopbackTarget+" "+h, "::1 "+h)
		}
		kept = append(kept, blockEnd)
	}
	return strings.Join(kept, eol) + eol
}

// Write replaces sudoku's block in the hosts file. Without permission it stages the new content in
// stage and returns ErrNeedsElevation with the command to install it.
func Write(hosts []string, stage string) (string, error) {
	path := HostsFile()
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	next := Apply(string(raw), hosts)
	if next == string(raw) {
		return "", nil
	}
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	// The hosts file is written in place (not renamed): on macOS/Windows it may be protected or
	// watched by inode, and a rename would drop its ACLs.
	if err := os.WriteFile(path, []byte(next), mode); err == nil {
		return "", nil
	} else if !errors.Is(err, fs.ErrPermission) {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(stage), 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(stage, []byte(next), 0o644); err != nil { //nolint:gosec // a copy of the public hosts file
		return "", fmt.Errorf("stage %s: %w", stage, err)
	}
	return InstallCommand(stage), ErrNeedsElevation
}

// InstallCommand is what the operator runs with elevated rights to install a staged hosts file.
func InstallCommand(stage string) string {
	if runtime.GOOS == osWindows {
		return fmt.Sprintf(`Copy-Item -Force '%s' "$env:SystemRoot\System32\drivers\etc\hosts"   # PowerShell от администратора`,
			strings.ReplaceAll(stage, "'", "''"))
	}
	return fmt.Sprintf("sudo cp '%s' %s", strings.ReplaceAll(stage, "'", `'"'"'`), HostsFile())
}
