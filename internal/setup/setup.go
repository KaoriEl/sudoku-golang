// Package setup installs sudoku on any OS: the binary into a bin directory on PATH, and into the
// sudoku home (~/.sudoku) the .env, sudoku-config/ and the infra compose.yaml with the container-name
// prefix. It can move an old install (infra inside the projects folder) into the home. Existing
// files are never overwritten unless asked (with a .bak copy); .env keys are patched in place.
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
	"sudoku-golang/internal/infra/configs"

	"github.com/joho/godotenv"
)

const (
	configDir   = configs.ConfigDirName
	composeName = configs.InfraComposeName
	envName     = configs.EnvFileName
	backupsDir  = "backups"
	goosWindows = "windows"
	goosDarwin  = "darwin"
)

var (
	ErrNoRoot      = errors.New("не задана папка с проектами: передайте --root")
	ErrRootMissing = errors.New("папка с проектами не найдена")
)

type Options struct {
	// Home is the sudoku home (default ~/.sudoku); Root the projects folder (absolute, must exist).
	Home   string
	Root   string
	Prefix string
	// BinDir receives the binary (default DefaultBinDir); SkipBinary leaves it where it is.
	BinDir     string
	SkipBinary bool
	// MigrateFrom is an old install (compose.yaml + sudoku-config/ inside the projects folder) whose
	// infra, configs, data and backups are moved into Home. Its containers must be removed first.
	MigrateFrom string
	// DockerPath / ComposePath go into .env when set (DOCKER_PATH, DOCKER_COMPOSE_PATH).
	DockerPath        string
	ComposePath       string
	OverwriteEnv      bool
	RegenerateCompose bool
}

// Step is one line of the report.
type Step struct {
	Done   bool
	Action string
	Path   string
}

func binaryName() string {
	if runtime.GOOS == goosWindows {
		return "sudoku.exe"
	}
	return "sudoku"
}

// DefaultBinDir is ~/.local/bin (%LOCALAPPDATA%\Programs\sudoku on Windows).
func DefaultBinDir() string {
	if runtime.GOOS == goosWindows {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "Programs", "sudoku")
		}
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".local", "bin")
}

// BinaryPath is where the installed binary lives.
func BinaryPath(binDir string) string {
	return filepath.Join(binDir, binaryName())
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

// IsLegacy reports an old install: dir holds a sudoku .env, compose.yaml and sudoku-config/.
func IsLegacy(dir string) bool {
	return dir != "" && configs.IsSudokuEnv(dir) && exists(filepath.Join(dir, composeName)) && exists(filepath.Join(dir, configDir))
}

type report struct{ steps []Step }

func (r *report) add(done bool, action, p string) {
	r.steps = append(r.steps, Step{Done: done, Action: action, Path: p})
}

// Run performs the setup and returns what it did.
func Run(o Options) ([]Step, error) {
	if o.Root == "" {
		return nil, ErrNoRoot
	}
	root := configs.ExpandPath(o.Root, "")
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrRootMissing, root)
	}
	home := o.Home
	if home == "" {
		home = configs.DefaultHome()
	}
	home = configs.ExpandPath(home, "")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", home, err)
	}
	r := &report{}

	if o.MigrateFrom != "" {
		if err := migrate(r, configs.ExpandPath(o.MigrateFrom, ""), home); err != nil {
			return r.steps, err
		}
	}

	if !o.SkipBinary {
		binDir := o.BinDir
		if binDir == "" {
			binDir = DefaultBinDir()
		}
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			return r.steps, fmt.Errorf("mkdir %s: %w", binDir, err)
		}
		target := BinaryPath(binDir)
		done, err := copySelf(target)
		if err != nil {
			return r.steps, err
		}
		r.add(done, "бинарник", target)
	}

	envTarget := filepath.Join(home, envName)
	if err := writeEnv(r, envTarget, root, o); err != nil {
		return r.steps, err
	}

	cfgTarget := filepath.Join(home, configDir)
	added, err := extractConfig(cfgTarget)
	if err != nil {
		return r.steps, err
	}
	switch {
	case added > 0:
		r.add(true, fmt.Sprintf("sudoku-config (новых файлов: %d)", added), cfgTarget)
	default:
		r.add(false, "sudoku-config уже на месте", cfgTarget)
	}

	composeTarget := filepath.Join(home, composeName)
	if exists(composeTarget) && !o.RegenerateCompose {
		r.add(false, "compose.yaml уже есть (перегенерировать: --regenerate-compose)", composeTarget)
		return r.steps, nil
	}
	src, err := os.ReadFile(filepath.Join(cfgTarget, composeName))
	if err != nil {
		return r.steps, fmt.Errorf("read %s: %w", filepath.Join(cfgTarget, composeName), err)
	}
	prefix := o.Prefix
	if prefix == "" {
		prefix = envValue(envTarget, "PREFIX_CONTAINER_NAME")
	}
	if err := backup(composeTarget); err != nil {
		return r.steps, err
	}
	if err := fsutil.WriteFileAtomicNoBackup(composeTarget, RewriteNames(src, prefix)); err != nil {
		return r.steps, err //nolint:wrapcheck // descriptive
	}
	r.add(true, "compose.yaml (префикс "+quoteEmpty(prefix)+")", composeTarget)
	return r.steps, nil
}

// writeEnv creates home/.env, or patches ROOT_PROJECTS_FOLDER (and the prefix, when given) in an
// existing one so the rest of the user's settings survive.
func writeEnv(r *report, target, root string, o Options) error {
	if exists(target) && !o.OverwriteEnv {
		return patchEnvFile(r, target, envKeys(root, o))
	}
	content, err := envContent(envKeys(root, o))
	if err != nil {
		return err
	}
	if err := backup(target); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomicNoBackup(target, content); err != nil {
		return err //nolint:wrapcheck // descriptive
	}
	r.add(true, ".env", target)
	return nil
}

type envKey struct{ key, value string }

// envKeys are the .env keys setup sets: the projects folder always, the rest when chosen.
func envKeys(root string, o Options) []envKey {
	keys := []envKey{{"ROOT_PROJECTS_FOLDER", root}}
	for _, k := range []envKey{{"PREFIX_CONTAINER_NAME", o.Prefix}, {"DOCKER_PATH", o.DockerPath}, {"DOCKER_COMPOSE_PATH", o.ComposePath}} {
		if k.value != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

func applyKeys(raw []byte, keys []envKey) []byte {
	for _, k := range keys {
		raw = setEnvKey(raw, k.key, k.value, true)
	}
	return raw
}

func patchEnvFile(r *report, target string, keys []envKey) error {
	raw, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read %s: %w", target, err)
	}
	out := applyKeys(raw, keys)
	if bytes.Equal(out, raw) {
		r.add(false, ".env без изменений", target)
		return nil
	}
	changed := []string{}
	for _, k := range keys {
		if envValueIn(raw, k.key) != k.value {
			changed = append(changed, k.key)
		}
	}
	if err := backup(target); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomicNoBackup(target, out); err != nil {
		return err //nolint:wrapcheck // descriptive
	}
	r.add(true, ".env обновлён ("+strings.Join(changed, ", ")+")", target)
	return nil
}

// migrate moves compose.yaml, sudoku-config/ and backups/ of an old install into home, and its .env
// when home has none. Nothing is overwritten: an entry already present in home is left in place.
func migrate(r *report, from, home string) error {
	if !IsLegacy(from) {
		return fmt.Errorf("%s не похожа на старую установку sudoku (нужны .env, compose.yaml и sudoku-config)", from)
	}
	if from == home {
		return nil
	}
	for _, name := range []string{composeName, configDir, backupsDir, envName} {
		src, dst := filepath.Join(from, name), filepath.Join(home, name)
		if !exists(src) {
			continue
		}
		if exists(dst) {
			r.add(false, "перенос: "+name+" уже есть в "+home+", оставлен в старой папке", src)
			continue
		}
		if err := move(src, dst); err != nil {
			return fmt.Errorf("перенос %s → %s: %w", src, dst, err)
		}
		r.add(true, "перенесено из "+from, dst)
	}
	return nil
}

// move renames, falling back to copy+remove across filesystems.
func move(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(os.DirFS(filepath.Dir(src)), filepath.Base(src), dst, true); err != nil {
		return err
	}
	return os.RemoveAll(src) //nolint:wrapcheck // caller wraps
}

// Leftovers lists the files an old install keeps after migrate (its binary, .env backups) — the
// folder can be deleted when nothing else is in it.
func Leftovers(dir string) (known []string, other []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		n := e.Name()
		switch {
		case n == "sudoku" || n == "sudoku.exe" || n == ".DS_Store" || n == envName ||
			strings.HasPrefix(n, envName+".") || strings.HasSuffix(n, ".bak"):
			known = append(known, filepath.Join(dir, n))
		default:
			other = append(other, filepath.Join(dir, n))
		}
	}
	return known, other
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
	if runtime.GOOS == goosWindows && exists(target) {
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
func envContent(keys []envKey) ([]byte, error) {
	if wd, err := os.Getwd(); err == nil && configs.IsSudokuEnv(wd) {
		raw, err := os.ReadFile(filepath.Join(wd, envName))
		if err != nil {
			return nil, fmt.Errorf("read .env: %w", err)
		}
		raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
		return applyKeys(raw, keys), nil
	}
	raw, err := sudokugolang.Defaults.ReadFile(".env.example")
	if err != nil {
		return nil, fmt.Errorf("env template: %w", err)
	}
	raw = setEnvKey(raw, "DOCKER_PATH", "docker", true)
	raw = setEnvKey(raw, "DOCKER_COMPOSE_PATH", "docker compose", true)
	return applyKeys(raw, keys), nil
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

// EnvValue reads one key from a dotenv file ("" when missing).
func EnvValue(path, key string) string {
	return envValue(path, key)
}

func envValue(path, key string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return envValueIn(raw, key)
}

func envValueIn(raw []byte, key string) string {
	values, err := godotenv.UnmarshalBytes(raw)
	if err != nil {
		return ""
	}
	return values[key]
}

// extractConfig fills target with the default sudoku-config (./sudoku-config when run from the
// repository, else the embedded copy). Files already present are kept — they may be edited by hand or
// by the admin (traefik ports) — so re-running setup only adds what is missing.
func extractConfig(target string) (int, error) {
	if exists("defaults.go") && exists(filepath.Join(configDir, composeName)) {
		return countNew(os.DirFS("."), configDir, target)
	}
	return countNew(sudokugolang.Defaults, configDir, target)
}

func countNew(src fs.FS, dir, target string) (int, error) {
	n := 0
	err := fs.WalkDir(src, dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !isData(p) && !exists(filepath.Join(target, filepath.FromSlash(strings.TrimPrefix(p, dir)))) {
			n++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("scan %s: %w", dir, err)
	}
	return n, copyTree(src, dir, target, false)
}

// isData is a database file under a data/ dir: never copied from a developer checkout.
func isData(p string) bool {
	d := path.Dir(p)
	return (strings.Contains(d, "/data/") || strings.HasSuffix(d, "/data")) && path.Base(p) != ".gitignore"
}

// copyTree copies dir of src into target. With all=false existing files are kept and data/ contents
// skipped; with all=true (migration) everything is copied.
func copyTree(src fs.FS, dir, target string, all bool) error {
	err := fs.WalkDir(src, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(target, filepath.FromSlash(strings.TrimPrefix(p, dir)))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !all && (isData(p) || exists(dst)) {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		in, err := src.Open(p)
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		defer func() { _ = in.Close() }()
		mode := fs.FileMode(0o644)
		if info, err := d.Info(); err == nil {
			mode = info.Mode().Perm() | 0o200
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err //nolint:wrapcheck // wrapped below
		}
		return out.Close()
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
