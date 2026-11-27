// Package backup dumps and restores the databases running in compose containers: PostgreSQL
// (pg_dumpall), MySQL/MariaDB (mysqldump/mariadb-dump), MongoDB (mongodump) and Redis/KeyDB/Valkey
// (RDB snapshot). Dumps stream from `docker exec` straight into a gzip file — nothing is buffered in
// memory — and every backup has a JSON sidecar with what is needed to restore it.
//
// Layout: <dir>/<container>/<20060102-150405>.<ext> + .json
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/engine"
	"time"
)

type Kind string

const (
	Postgres Kind = "postgres"
	MySQL    Kind = "mysql"
	Mongo    Kind = "mongo"
	Redis    Kind = "redis"

	stampLayout   = "20060102-150405"
	stderrLimit   = 4096
	redisStopWait = 20
	metaExt       = ".json"
)

var (
	ErrUnsupported = errors.New("контейнер не похож на поддерживаемую базу данных")
	ErrNotRunning  = errors.New("контейнер не запущен")
	ErrBadID       = errors.New("неверный идентификатор бэкапа")
	nameSafe       = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
	idRe           = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[0-9]{8}-[0-9]{6}$`)
)

var kindWords = []struct {
	kind  Kind
	words []string
}{
	{Postgres, []string{"postgres", "postgis", "timescale", "pgvector", "pgsql"}},
	{MySQL, []string{"mariadb", "mysql", "percona"}},
	{Mongo, []string{"mongo"}},
	{Redis, []string{"redis", "keydb", "valkey", "dragonfly"}},
}

// Detect guesses the database kind from the image repository (not the tag, not the container name:
// "api-postgres-client" must not count).
func Detect(image string) (Kind, bool) {
	repo := strings.ToLower(image)
	repo, _, _ = strings.Cut(repo, "@")
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	for _, kw := range kindWords {
		for _, w := range kw.words {
			if strings.Contains(repo, w) {
				return kw.kind, true
			}
		}
	}
	return "", false
}

func ext(k Kind) string {
	switch k {
	case Mongo:
		return ".archive.gz"
	case Redis:
		return ".rdb"
	case Postgres, MySQL:
		return ".sql.gz"
	}
	return ".sql.gz"
}

// Backup is one stored dump.
type Backup struct {
	ID          string    `json:"id"`
	Container   string    `json:"container"`
	Project     string    `json:"project"`
	Service     string    `json:"service"`
	ComposeFile string    `json:"composeFile"`
	Kind        Kind      `json:"kind"`
	Image       string    `json:"image"`
	File        string    `json:"file"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"createdAt"`
	RedisDir    string    `json:"redisDir,omitempty"`
	RedisFile   string    `json:"redisFile,omitempty"`
}

type Manager struct {
	log    *slog.Logger
	eng    *engine.Engine
	runner *docker.Runner
	dir    func() string
}

func NewManager(log *slog.Logger, eng *engine.Engine, runner *docker.Runner, dir func() string) *Manager {
	return &Manager{log: log, eng: eng, runner: runner, dir: dir}
}

func (m *Manager) Dir() string {
	return m.dir()
}

func containerName(c docker.Container) string {
	return strings.TrimPrefix(c.Name, "/")
}

func safeName(s string) string {
	s = nameSafe.ReplaceAllString(s, "_")
	if s == "" || s == "." || s == ".." {
		return "container"
	}
	return s
}

// List returns all backups, newest first.
func (m *Manager) List() ([]Backup, error) {
	root := m.dir()
	var out []Backup
	dirs, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(root, d.Name()))
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), metaExt) {
				continue
			}
			if b, err := m.readMeta(filepath.Join(root, d.Name(), f.Name())); err == nil {
				out = append(out, b)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if out == nil {
		out = []Backup{}
	}
	return out, nil
}

func (m *Manager) readMeta(p string) (Backup, error) {
	raw, err := os.ReadFile(p)
	if err != nil {
		return Backup{}, fmt.Errorf("read meta: %w", err)
	}
	var b Backup
	if err := json.Unmarshal(raw, &b); err != nil {
		return Backup{}, fmt.Errorf("parse meta: %w", err)
	}
	b.File = filepath.Join(filepath.Dir(p), filepath.Base(b.File))
	if st, err := os.Stat(b.File); err == nil {
		b.Size = st.Size()
	} else {
		return Backup{}, fmt.Errorf("dump missing: %w", err)
	}
	return b, nil
}

// Get resolves an ID ("<container>/<stamp>") to its backup, refusing anything outside the backup dir.
func (m *Manager) Get(id string) (Backup, error) {
	if !idRe.MatchString(id) {
		return Backup{}, ErrBadID
	}
	dirName, stamp := path.Split(id)
	return m.readMeta(filepath.Join(m.dir(), strings.TrimSuffix(dirName, "/"), stamp+metaExt))
}

func (m *Manager) Delete(id string) error {
	b, err := m.Get(id)
	if err != nil {
		return err
	}
	_ = os.Remove(b.File)
	if err := os.Remove(strings.TrimSuffix(b.File, ext(b.Kind)) + metaExt); err != nil {
		return fmt.Errorf("delete backup: %w", err)
	}
	return nil
}

// Prune keeps the newest keep backups of a container.
func (m *Manager) Prune(container string, keep int) {
	all, err := m.List()
	if err != nil {
		return
	}
	n := 0
	for _, b := range all {
		if b.Container != container {
			continue
		}
		n++
		if n > keep {
			_ = m.Delete(b.ID)
		}
	}
}

func execArgs(c docker.Container, interactive bool, script string) []string {
	args := []string{"exec"}
	if interactive {
		args = append(args, "-i")
	}
	return append(args, c.ID, "sh", "-c", script)
}

const (
	pgDump    = `exec pg_dumpall -U "${POSTGRES_USER:-postgres}" --clean --if-exists`
	pgRestore = `exec psql -U "${POSTGRES_USER:-postgres}" -d postgres -q -v ON_ERROR_STOP=0`
	myDump    = `D=$(command -v mariadb-dump || command -v mysqldump); P="${MARIADB_ROOT_PASSWORD:-$MYSQL_ROOT_PASSWORD}"; ` +
		`exec "$D" --all-databases --single-transaction --routines --events --triggers -uroot ${P:+-p"$P"}`
	myRestore = `C=$(command -v mariadb || command -v mysql); P="${MARIADB_ROOT_PASSWORD:-$MYSQL_ROOT_PASSWORD}"; exec "$C" -uroot ${P:+-p"$P"}`
	mongoAuth = `${MONGO_INITDB_ROOT_USERNAME:+--username "$MONGO_INITDB_ROOT_USERNAME" ` +
		`--password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin}`
	moDump    = `exec mongodump --archive --gzip ` + mongoAuth
	moRestore = `exec mongorestore --archive --gzip --drop ` + mongoAuth
	redisCLI  = `C=$(command -v redis-cli || command -v keydb-cli || command -v valkey-cli); A="${REDIS_PASSWORD:-}"; ` +
		`r() { if [ -n "$A" ]; then "$C" -a "$A" --no-auth-warning "$@"; else "$C" "$@"; fi; }; `
	redisLocate = redisCLI + `r SAVE >/dev/null && printf '%s\n%s\n' "$(r CONFIG GET dir | tail -n1)" "$(r CONFIG GET dbfilename | tail -n1)"`
)

func dumpScript(k Kind) string {
	switch k {
	case Postgres:
		return pgDump
	case MySQL:
		return myDump
	case Mongo:
		return moDump
	case Redis:
		return ""
	}
	return ""
}

func restoreScript(k Kind) string {
	switch k {
	case Postgres:
		return pgRestore
	case MySQL:
		return myRestore
	case Mongo:
		return moRestore
	case Redis:
		return ""
	}
	return ""
}

// stream runs `docker exec` with stdout to w (and optional stdin), returning stderr's tail on failure.
func (m *Manager) stream(ctx context.Context, c docker.Container, script string, stdin io.Reader, w io.Writer) error {
	cmd, err := m.runner.Command(ctx, m.runner.DockerArgv(), execArgs(c, stdin != nil, script)...)
	if err != nil {
		return err //nolint:wrapcheck // ErrEmptyCommand
	}
	errBuf := &docker.TailBuffer{Limit: stderrLimit}
	cmd.Stdout = w
	cmd.Stderr = errBuf
	cmd.Stdin = stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return nil
}

type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err //nolint:wrapcheck // passthrough
}

// Create dumps the database of c into the backup dir. log receives progress lines.
func (m *Manager) Create(ctx context.Context, c docker.Container, log io.Writer) (Backup, error) {
	kind, ok := Detect(c.Config.Image)
	if !ok {
		return Backup{}, ErrUnsupported
	}
	if !c.State.Running {
		return Backup{}, ErrNotRunning
	}
	name := containerName(c)
	now := time.Now()
	stamp := now.Format(stampLayout)
	dir := filepath.Join(m.dir(), safeName(name))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Backup{}, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	b := Backup{
		ID: safeName(name) + "/" + stamp, Container: name, Kind: kind, Image: c.Config.Image, CreatedAt: now.UTC(),
		Project: c.Config.Labels["com.docker.compose.project"], Service: c.Config.Labels["com.docker.compose.service"],
		ComposeFile: c.Config.Labels["com.docker.compose.project.config_files"],
		File:        filepath.Join(dir, stamp+ext(kind)),
	}
	_, _ = fmt.Fprintf(log, "%s: %s → %s\n", name, kind, b.File)

	tmp := b.File + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return Backup{}, fmt.Errorf("create %s: %w", tmp, err)
	}
	defer func() { _ = os.Remove(tmp) }()
	cnt := &counter{w: f}
	switch kind {
	case Redis:
		err = m.dumpRedis(ctx, c, cnt, &b, log)
	case Mongo:
		err = m.stream(ctx, c, dumpScript(kind), nil, cnt)
	case Postgres, MySQL:
		gz, _ := gzip.NewWriterLevel(cnt, gzip.BestSpeed)
		err = m.stream(ctx, c, dumpScript(kind), nil, gz)
		if cerr := gz.Close(); err == nil {
			err = cerr
		}
	}
	if serr := f.Sync(); err == nil {
		err = serr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Backup{}, fmt.Errorf("dump %s: %w", name, err)
	}
	if err := os.Rename(tmp, b.File); err != nil {
		return Backup{}, fmt.Errorf("finalize %s: %w", b.File, err)
	}
	b.Size = cnt.n
	meta, _ := json.MarshalIndent(b, "", "  ")
	if err := os.WriteFile(strings.TrimSuffix(b.File, ext(kind))+metaExt, meta, 0o600); err != nil {
		return Backup{}, fmt.Errorf("write meta: %w", err)
	}
	_, _ = fmt.Fprintf(log, "готово: %d байт\n", b.Size)
	return b, nil
}

func (m *Manager) redisLocation(ctx context.Context, c docker.Container) (string, string, error) {
	var out bytes.Buffer
	if err := m.stream(ctx, c, redisLocate, nil, &out); err != nil {
		return "", "", err
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 || lines[0] == "" || lines[1] == "" {
		return "", "", fmt.Errorf("redis: не удалось узнать dir/dbfilename: %q", out.String())
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), nil
}

// dumpRedis runs SAVE, then copies the RDB file out through the engine's archive API (no shell
// quoting of paths, works for every redis-compatible server).
func (m *Manager) dumpRedis(
	ctx context.Context, c docker.Container, w io.Writer, b *Backup, log io.Writer,
) error {
	dir, file, err := m.redisLocation(ctx, c)
	if err != nil {
		return err
	}
	b.RedisDir, b.RedisFile = dir, file
	_, _ = fmt.Fprintf(log, "SAVE → %s/%s\n", dir, file)
	rc, err := m.eng.GetArchive(ctx, c.ID, path.Join(dir, file))
	if err != nil {
		return fmt.Errorf("get rdb: %w", err)
	}
	defer func() { _ = rc.Close() }()
	tr := tar.NewReader(rc)
	for {
		h, err := tr.Next()
		if err != nil {
			return fmt.Errorf("rdb archive: %w", err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := io.Copy(w, tr); err != nil { //nolint:gosec // size bounded by the container's own file
				return fmt.Errorf("copy rdb: %w", err)
			}
			return nil
		}
	}
}

// Restore loads b into container c. SQL and Mongo dumps are fed to the client inside the container;
// an RDB snapshot needs the server stopped, the file replaced and the server started again.
func (m *Manager) Restore(ctx context.Context, b Backup, c docker.Container, log io.Writer) error {
	kind, ok := Detect(c.Config.Image)
	if !ok || kind != b.Kind {
		return fmt.Errorf("%w: бэкап %s, контейнер %s", ErrUnsupported, b.Kind, c.Config.Image)
	}
	f, err := os.Open(b.File)
	if err != nil {
		return fmt.Errorf("open %s: %w", b.File, err)
	}
	defer func() { _ = f.Close() }()
	name := containerName(c)
	_, _ = fmt.Fprintf(log, "%s ← %s\n", name, b.File)

	switch kind {
	case Redis:
		return m.restoreRedis(ctx, b, c, f, log)
	case Mongo:
		if !c.State.Running {
			return ErrNotRunning
		}
		return m.stream(ctx, c, restoreScript(kind), f, log)
	case Postgres, MySQL:
		if !c.State.Running {
			return ErrNotRunning
		}
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("gunzip: %w", err)
		}
		defer func() { _ = gz.Close() }()
		return m.stream(ctx, c, restoreScript(kind), gz, &docker.TailBuffer{Limit: stderrLimit})
	}
	return ErrUnsupported
}

func (m *Manager) restoreRedis(ctx context.Context, b Backup, c docker.Container, f *os.File, log io.Writer) error {
	dir, file := b.RedisDir, b.RedisFile
	if dir == "" || file == "" {
		return errors.New("в бэкапе нет пути к RDB-файлу")
	}
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	_, _ = fmt.Fprintln(log, "останавливаю сервер, чтобы он не перезаписал файл при выходе")
	if err := m.eng.ContainerAction(ctx, c.ID, "stop", redisStopWait); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		err := tw.WriteHeader(&tar.Header{Name: file, Mode: 0o644, Size: st.Size(), ModTime: time.Now(), Typeflag: tar.TypeReg})
		if err == nil {
			_, err = io.Copy(tw, f)
		}
		if err == nil {
			err = tw.Close()
		}
		_ = pw.CloseWithError(err)
	}()
	putErr := m.eng.PutArchive(ctx, c.ID, dir, pr)
	_ = pr.Close()
	_, _ = fmt.Fprintln(log, "запускаю сервер")
	if err := m.eng.ContainerAction(ctx, c.ID, "start", 0); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if putErr != nil {
		return fmt.Errorf("put rdb: %w", putErr)
	}
	_, _ = fmt.Fprintln(log, "готово (если включён appendonly, сервер загрузит AOF, а не RDB)")
	return nil
}
