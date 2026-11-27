// Package doctor diagnoses a sudoku setup: config, docker CLI and compose, the Engine API endpoint,
// every compose file, host port conflicts, file permissions and optional tools. Used by
// `sudoku doctor` and the admin's settings page.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sudoku-golang/internal/containers"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/engine"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/notify"
	"sudoku-golang/internal/portcheck"
	"sudoku-golang/internal/prefs"
	"sudoku-golang/internal/service"
	"sudoku-golang/internal/version"
	"sync"
	"time"
)

type Level string

const (
	OK   Level = "ok"
	Info Level = "info"
	Warn Level = "warn"
	Fail Level = "fail"

	checkTimeout    = 20 * time.Second
	minComposeMajor = 2
	minComposeMinor = 20
)

const (
	groupConfig   = "Конфиг"
	groupDocker   = "Docker"
	groupCompose  = "Compose"
	groupEngine   = "Engine API"
	groupProjects = "Проекты"
	groupPorts    = "Порты"
	groupFiles    = "Файлы"
	groupTools    = "Инструменты"
)

type Check struct {
	Group  string `json:"group"`
	Title  string `json:"title"`
	Level  Level  `json:"level"`
	Detail string `json:"detail"`
	Hint   string `json:"hint"`
}

type Report struct {
	Checks  []Check   `json:"checks"`
	Fails   int       `json:"fails"`
	Warns   int       `json:"warns"`
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

type Env struct {
	Cfg    *configs.Config
	Runner *docker.Runner
	Engine *engine.Engine
}

type collector struct {
	mu     sync.Mutex
	checks []Check
}

func (c *collector) add(ch Check) {
	c.mu.Lock()
	c.checks = append(c.checks, ch)
	c.mu.Unlock()
}

func short(err error) string {
	s := strings.TrimSpace(err.Error())
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	return s
}

// Run executes every check. Independent checks run concurrently; the report keeps a stable order.
func Run(ctx context.Context, env Env) Report {
	c := &collector{}
	l := service.LayoutOf(env.Cfg)

	c.add(checkEnvFile(env.Cfg))
	files := checkRoot(c, env.Cfg, l)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); checkDocker(ctx, c, env) }()
	go func() { defer wg.Done(); checkCompose(ctx, c, env) }()
	go func() { defer wg.Done(); checkEngine(ctx, c, env) }()
	wg.Wait()
	checkFiles(ctx, c, env, files)
	checkPorts(ctx, c, env, files)
	checkWritable(c, l)
	checkStores(c, l)
	checkTools(c)

	order := map[string]int{
		groupConfig: 0, groupDocker: 1, groupCompose: 2, groupEngine: 3, groupProjects: 4, groupPorts: 5, groupFiles: 6, groupTools: 7,
	}
	rep := Report{Checks: c.checks, Version: version.Get().String(), At: time.Now().UTC()}
	sortChecks(rep.Checks, order)
	for _, ch := range rep.Checks {
		switch ch.Level {
		case Fail:
			rep.Fails++
		case Warn:
			rep.Warns++
		case OK, Info:
		}
	}
	return rep
}

func sortChecks(list []Check, order map[string]int) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && order[list[j].Group] < order[list[j-1].Group]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func checkEnvFile(cfg *configs.Config) Check {
	ch := Check{Group: groupConfig, Title: ".env"}
	p := filepath.Join(cfg.Home, configs.EnvFileName)
	if _, err := os.Stat(p); err != nil {
		ch.Level, ch.Detail = Warn, "нет "+p+" — используются переменные окружения и значения по умолчанию"
		ch.Hint = "sudoku setup создаст его"
		return ch
	}
	ch.Level, ch.Detail = OK, p
	return ch
}

func checkRoot(c *collector, cfg *configs.Config, l service.Layout) []string {
	infra := Check{Group: groupConfig, Title: "инфраструктура"}
	switch {
	case !fileExists(l.InfraFile()):
		infra.Level, infra.Detail, infra.Hint = Warn, "нет "+l.InfraFile(), "sudoku setup создаст его"
	case cfg.Legacy():
		infra.Level, infra.Detail = Warn, l.InfraFile()+" — старая раскладка: инфраструктура внутри папки проектов"
		infra.Hint = "sudoku setup перенесёт её в " + configs.DefaultHome()
	default:
		infra.Level, infra.Detail = OK, l.InfraFile()
	}
	c.add(infra)

	ch := Check{Group: groupConfig, Title: "ROOT_PROJECTS_FOLDER"}
	root := l.Root
	if root == "" {
		ch.Level, ch.Detail, ch.Hint = Fail, "не задан", "sudoku setup или ROOT_PROJECTS_FOLDER в "+configs.EnvPath()
		c.add(ch)
		return nil
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		ch.Level, ch.Detail, ch.Hint = Fail, "папка "+root+" не найдена", "укажите ROOT_PROJECTS_FOLDER в "+configs.EnvPath()
		c.add(ch)
		return nil
	}
	files, err := l.Discover()
	projects := 0
	for _, f := range files {
		if !l.IsInfra(f) {
			projects++
		}
	}
	switch {
	case err != nil:
		ch.Level, ch.Detail = Fail, short(err)
	case projects == 0:
		ch.Level, ch.Detail = Warn, root+": проекты не найдены"
		ch.Hint = "sudoku ищет <проект>/compose-sudoku.yaml в этой папке"
	default:
		ch.Level, ch.Detail = OK, fmt.Sprintf("%s: %d проектов", root, projects)
	}
	c.add(ch)
	return files
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func run(ctx context.Context, env Env, base []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	cmd, err := env.Runner.Command(ctx, base, args...)
	if err != nil {
		return "", err //nolint:wrapcheck // ErrEmptyCommand
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func checkDocker(ctx context.Context, c *collector, env Env) {
	ch := Check{Group: groupDocker, Title: "docker CLI"}
	argv := env.Runner.DockerArgv()
	bin := ""
	if len(argv) > 0 {
		bin = argv[0]
		if filepath.Base(bin) == "env" && len(argv) > 1 {
			bin = argv[1]
		}
	}
	if _, err := exec.LookPath(bin); err != nil {
		ch.Level, ch.Detail, ch.Hint = Fail, fmt.Sprintf("%q не найден в PATH", bin), "установите Docker или поправьте DOCKER_PATH"
		c.add(ch)
		return
	}
	out, err := run(ctx, env, argv, "version", "--format", "{{.Client.Version}} / сервер {{.Server.Version}}")
	if err != nil {
		ch.Level, ch.Detail, ch.Hint = Fail, out, "запущен ли Docker (Docker Desktop / Colima / OrbStack / dockerd)?"
		c.add(ch)
		return
	}
	ch.Level, ch.Detail = OK, out
	c.add(ch)
}

func parseComposeVersion(s string) (int, int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(strings.TrimFunc(parts[1], func(r rune) bool { return r < '0' || r > '9' }))
	return major, minor, err1 == nil && err2 == nil
}

func checkCompose(ctx context.Context, c *collector, env Env) {
	ch := Check{Group: groupCompose, Title: "docker compose"}
	out, err := run(ctx, env, env.Runner.ComposeArgv(), "version", "--short")
	if err != nil {
		ch.Level, ch.Detail = Fail, out
		ch.Hint = "установите compose plugin или поправьте DOCKER_COMPOSE_PATH (\"docker compose\" / \"docker-compose\")"
		c.add(ch)
		return
	}
	ch.Detail = strings.Join(env.Runner.ComposeArgv(), " ") + " " + out
	major, minor, ok := parseComposeVersion(out)
	switch {
	case !ok:
		ch.Level = Info
	case major < minComposeMajor || (major == minComposeMajor && minor < minComposeMinor):
		ch.Level = Warn
		ch.Hint = fmt.Sprintf("рекомендуется compose ≥ %d.%d (выборочный down, --wait, config --no-interpolate)", minComposeMajor, minComposeMinor)
	default:
		ch.Level = OK
	}
	c.add(ch)
}

func checkEngine(ctx context.Context, c *collector, env Env) {
	ch := Check{Group: groupEngine, Title: "сокет Docker Engine"}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	ep := env.Engine.Socket()
	if err := env.Engine.Ping(ctx); err != nil {
		ch.Level, ch.Detail = Warn, ep+": "+short(err)
		ch.Hint = "админка перейдёт на docker CLI (медленнее), метрики и терминал не будут работать; проверьте DOCKER_HOST / docker context"
		c.add(ch)
		return
	}
	v, err := env.Engine.Version(ctx)
	ch.Level, ch.Detail = OK, ep
	if err == nil {
		ch.Detail += fmt.Sprintf(" · Engine %s (API %s, %s/%s)", v.Version, v.APIVersion, v.Os, v.Arch)
	}
	c.add(ch)
}

func checkFiles(ctx context.Context, c *collector, env Env, files []string) {
	workers := max(1, env.Cfg.MaxWorkers)
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	l := service.LayoutOf(env.Cfg)
	for _, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(f string) {
			defer wg.Done()
			defer func() { <-sem }()
			ch := Check{Group: groupProjects, Title: l.Rel(f)}
			out, err := run(ctx, env, env.Runner.ComposeArgv(), "--file", f, "config", "--quiet")
			switch {
			case err != nil:
				ch.Level, ch.Detail = Fail, out
			case strings.Contains(out, "variable is not set"):
				ch.Level, ch.Detail, ch.Hint = Warn, "не заданы: "+strings.Join(unsetVars(out), ", "), "задайте переменные в .env проекта"
			default:
				ch.Level, ch.Detail = OK, "валиден"
			}
			c.add(ch)
		}(f)
	}
	wg.Wait()
}

func checkPorts(ctx context.Context, c *collector, env Env, files []string) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout*2)
	defer cancel()
	src := containers.NewSource(slog.New(slog.DiscardHandler), env.Engine, env.Runner)
	running, err := src.List(ctx)
	if err != nil {
		return
	}
	checked, conflicts := 0, 0
	for _, f := range files {
		raw, err := env.Runner.ComposeConfigJSON(ctx, f)
		if err != nil {
			continue
		}
		bindings, err := portcheck.Published(raw, f, nil)
		if err != nil {
			continue
		}
		checked += len(bindings)
		for _, cf := range portcheck.Check(ctx, bindings, running) {
			conflicts++
			c.add(Check{
				Group: groupPorts, Title: fmt.Sprintf("%d/%s", cf.Port, cf.Proto), Level: Warn, Detail: cf.String(),
				Hint: "остановите владельца порта или поменяйте published-порт",
			})
		}
	}
	if conflicts == 0 {
		c.add(Check{
			Group: groupPorts, Title: "опубликованные порты", Level: OK,
			Detail: fmt.Sprintf("%d портов свободны или заняты своими сервисами", checked),
		})
	}
}

func checkWritable(c *collector, l service.Layout) {
	dir := l.ConfigDir()
	ch := Check{Group: groupFiles, Title: "sudoku-config"}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		ch.Level, ch.Detail = Fail, short(err)
		c.add(ch)
		return
	}
	f, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		ch.Level, ch.Detail, ch.Hint = Fail, short(err), "нет прав на запись: настройки, наборы и история не сохранятся"
		c.add(ch)
		return
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	ch.Level, ch.Detail = OK, dir+" доступна для записи"
	c.add(ch)
}

func checkStores(c *collector, l service.Layout) {
	if _, err := prefs.Load(prefs.Path(l.Data)); err != nil {
		c.add(Check{Group: groupFiles, Title: "admin.yaml", Level: Fail, Detail: short(err)})
	}
	if _, err := service.NewDisabledStore(l).For(""); err != nil {
		c.add(Check{Group: groupFiles, Title: "disabled.yaml", Level: Fail, Detail: short(err)})
	}
	if _, err := service.NewSetsStore(l).List(); err != nil {
		c.add(Check{Group: groupFiles, Title: "sets.yaml", Level: Fail, Detail: short(err)})
	}
}

func checkTools(c *collector) {
	if p, err := exec.LookPath("mkcert"); err == nil {
		c.add(Check{Group: groupTools, Title: "mkcert", Level: OK, Detail: p})
	} else {
		c.add(Check{Group: groupTools, Title: "mkcert", Level: Info, Detail: "не установлен — нужен только для локального HTTPS"})
	}
	if ok, why := notify.Supported(); ok {
		c.add(Check{Group: groupTools, Title: "уведомления", Level: OK, Detail: runtime.GOOS})
	} else {
		c.add(Check{Group: groupTools, Title: "уведомления", Level: Info, Detail: why})
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		c.add(Check{Group: groupTools, Title: "WSL", Level: Info, Detail: "WSL " + os.Getenv("WSL_DISTRO_NAME") +
			": админка доступна из Windows по http://localhost:<порт> (localhost forwarding)"})
	}
}

// ErrFailed is returned by the CLI when the report has failures.
var ErrFailed = errors.New("doctor found problems")

var unsetVarRe = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*)\\?" variable is not set`)

// unsetVars extracts the distinct variable names from compose's "variable is not set" warnings.
func unsetVars(out string) []string {
	seen := map[string]bool{}
	var names []string
	for _, m := range unsetVarRe.FindAllStringSubmatch(out, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	return names
}
