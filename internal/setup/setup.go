// Package setup bootstraps ROOT_PROJECTS_FOLDER on any OS — the deploy part of build.sh in Go:
// copy the binary and .env, lay out sudoku-config/ and generate the root compose.yaml with the
// container-name prefix. Existing files are never overwritten unless asked (with a .bak copy).
package setup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	sudokugolang "sudoku-golang"
	"sudoku-golang/internal/fsutil"
)

const (
	configDir   = "sudoku-config"
	composeName = "compose.yaml"
	envName     = ".env"
)

var ErrNoRoot = errors.New("не задан ROOT_PROJECTS_FOLDER: передайте --root или создайте .env")

type Options struct {
	Root              string
	Prefix            string
	OverwriteEnv      bool
	RegenerateCompose bool
	SkipBinary        bool
}

// Step is one line of the report.
type Step struct {
	Done   bool
	Action string
	Path   string
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "sudoku.exe"
	}
	return "sudoku"
}

func backup(p string) error {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil //nolint:nilerr // nothing to back up
	}
	if err := os.WriteFile(p+".bak", raw, 0o644); err != nil { //nolint:gosec // backup next to the original
		return fmt.Errorf("backup %s: %w", p, err)
	}
	return nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Run performs the setup and returns what it did.
func Run(o Options) ([]Step, error) {
	if o.Root == "" {
		return nil, ErrNoRoot
	}
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return nil, fmt.Errorf("root: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", root, err)
	}
	var steps []Step
	add := func(done bool, action, p string) { steps = append(steps, Step{Done: done, Action: action, Path: p}) }

	if !o.SkipBinary {
		target := filepath.Join(root, binaryName())
		done, err := copySelf(target)
		if err != nil {
			return steps, err
		}
		add(done, "бинарник", target)
	}

	envTarget := filepath.Join(root, envName)
	switch {
	case exists(envTarget) && !o.OverwriteEnv:
		add(false, ".env уже есть (перезаписать: --overwrite-env)", envTarget)
	default:
		content, err := envContent(root, o.Prefix)
		if err != nil {
			return steps, err
		}
		if err := backup(envTarget); err != nil {
			return steps, err
		}
		if err := fsutil.WriteFileAtomicNoBackup(envTarget, content); err != nil {
			return steps, err //nolint:wrapcheck // descriptive
		}
		add(true, ".env", envTarget)
	}

	cfgTarget := filepath.Join(root, configDir)
	if exists(cfgTarget) {
		add(false, "sudoku-config уже есть", cfgTarget)
	} else {
		if err := extractConfig(cfgTarget); err != nil {
			return steps, err
		}
		add(true, "sudoku-config", cfgTarget)
	}

	composeTarget := filepath.Join(root, composeName)
	if exists(composeTarget) && !o.RegenerateCompose {
		add(false, "compose.yaml уже есть (перегенерировать: --regenerate-compose)", composeTarget)
		return steps, nil
	}
	src, err := os.ReadFile(filepath.Join(cfgTarget, composeName))
	if err != nil {
		return steps, fmt.Errorf("read %s: %w", filepath.Join(cfgTarget, composeName), err)
	}
	prefix := o.Prefix
	if prefix == "" {
		prefix = envValue(envTarget, "PREFIX_CONTAINER_NAME")
	}
	if err := backup(composeTarget); err != nil {
		return steps, err
	}
	if err := fsutil.WriteFileAtomicNoBackup(composeTarget, RewriteNames(src, prefix)); err != nil {
		return steps, err //nolint:wrapcheck // descriptive
	}
	add(true, "compose.yaml (префикс "+quoteEmpty(prefix)+")", composeTarget)
	return steps, nil
}

func quoteEmpty(s string) string {
	if s == "" {
		return "пустой"
	}
	return s
}

func copySelf(target string) (bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if t, err := filepath.EvalSymlinks(target); err == nil && t == exe {
		return false, nil // running the deployed binary itself
	}
	in, err := os.Open(exe)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", exe, err)
	}
	defer func() { _ = in.Close() }()
	tmp := target + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return false, fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return false, fmt.Errorf("copy binary: %w", err)
	}
	if err := out.Close(); err != nil {
		return false, fmt.Errorf("close: %w", err)
	}
	if runtime.GOOS == "windows" && exists(target) {
		_ = os.Remove(target + ".old")
		if err := os.Rename(target, target+".old"); err != nil {
			return false, fmt.Errorf("move old binary: %w", err)
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		return false, fmt.Errorf("install binary: %w", err)
	}
	return true, nil
}

// envContent is the repository's .env if the current directory has one (CRLF stripped, like
// build.sh did with sed), otherwise the embedded template filled with root and prefix.
func envContent(root, prefix string) ([]byte, error) {
	if raw, err := os.ReadFile(envName); err == nil {
		raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
		return setEnvKey(raw, "ROOT_PROJECTS_FOLDER", root, false), nil
	}
	raw, err := sudokugolang.Defaults.ReadFile(".env.example")
	if err != nil {
		return nil, fmt.Errorf("env template: %w", err)
	}
	out := setEnvKey(raw, "ROOT_PROJECTS_FOLDER", root, true)
	out = setEnvKey(out, "DOCKER_PATH", "docker", true)
	out = setEnvKey(out, "DOCKER_COMPOSE_PATH", "docker compose", true)
	if prefix != "" {
		out = setEnvKey(out, "PREFIX_CONTAINER_NAME", prefix, true)
	}
	return out, nil
}

func envLine(key, value string) string {
	if strings.ContainsAny(value, " #\"'$\\") {
		return key + `="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
	}
	return key + "=" + value
}

// setEnvKey sets key in a dotenv file; when force is false an existing non-empty value wins.
func setEnvKey(raw []byte, key, value string, force bool) []byte {
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	found := false
	for i, l := range lines {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		found = true
		if force || strings.TrimSpace(v) == "" {
			lines[i] = envLine(key, value)
		}
	}
	if !found {
		lines = append(lines, envLine(key, value))
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func envValue(path, key string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// extractConfig copies ./sudoku-config when run from the repository, else the embedded defaults.
func extractConfig(target string) error {
	if st, err := os.Stat(configDir); err == nil && st.IsDir() {
		return copyTree(os.DirFS(configDir), target)
	}
	sub, err := fs.Sub(sudokugolang.Defaults, configDir)
	if err != nil {
		return fmt.Errorf("embedded config: %w", err)
	}
	return copyTree(sub, target)
}

func copyTree(src fs.FS, target string) error {
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(target, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if strings.Contains(path.Dir(p), "/data/") || strings.HasSuffix(path.Dir(p), "/data") {
			if path.Base(p) != ".gitignore" {
				return nil // never copy database files from a developer checkout
			}
		}
		raw, err := fs.ReadFile(src, p)
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		return os.WriteFile(dst, raw, 0o644) //nolint:gosec // config files
	})
	if err != nil {
		return fmt.Errorf("copy %s: %w", target, err)
	}
	return nil
}

// RewriteNames applies the container-name prefix: `container_name: X_traefik` becomes
// `container_name: <prefix>_traefik` (the last "_" segment is the service part), hostnames are
// unquoted — the same transformation build.sh did with awk.
func RewriteNames(src []byte, prefix string) []byte {
	lines := strings.Split(string(src), "\n")
	for i, l := range lines {
		trimmed := strings.TrimLeft(l, " \t")
		indent := l[:len(l)-len(trimmed)]
		for _, key := range []string{"container_name:", "hostname:"} {
			if !strings.HasPrefix(trimmed, key) {
				continue
			}
			val := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, key)), `"'`)
			if key == "container_name:" {
				parts := strings.Split(val, "_")
				val = parts[len(parts)-1]
				if prefix != "" {
					val = prefix + "_" + val
				}
			}
			lines[i] = indent + key + " " + val
		}
	}
	return []byte(strings.Join(lines, "\n"))
}
