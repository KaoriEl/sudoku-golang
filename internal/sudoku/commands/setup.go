package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/setup"
	"time"

	"github.com/alperdrsnn/clime"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	defaultPrefix   = "dev"
	labelComposeCfg = "com.docker.compose.project.config_files"
	dockerTimeout   = 2 * time.Minute
)

var errSetupCancelled = errors.New("установка отменена")

type setupFlags struct {
	o         setup.Options
	yes       bool
	noMigrate bool
	noPath    bool
}

func commandSetup() *cobra.Command {
	f := &setupFlags{}
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Установить sudoku: бинарник в PATH, настройки и инфраструктура в ~/.sudoku",
		Long: "Без флагов в терминале задаёт пару вопросов (папка с проектами, префикс контейнеров) и показывает,\n" +
			"что и куда будет установлено. Бинарник копируется в " + displayPath(setup.DefaultBinDir()) + ", .env, compose.yaml\n" +
			"инфраструктуры и sudoku-config/ — в " + displayPath(configs.DefaultHome()) + ". Старую установку (всё внутри папки проектов)\n" +
			"находит сама и предлагает перенести вместе с данными. Повторный запуск безопасен: существующие\n" +
			"файлы не перезаписываются, в .env меняется только папка проектов (и префикс, если задан).",
		Example: "  sudoku setup\n  sudoku setup --root ~/projects --prefix dev --yes",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			banner("setup")
			return runSetup(cmd.Context(), f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.o.Root, "root", "", "Папка с проектами (в ней ищутся <проект>/compose-sudoku.yaml)")
	fl.StringVar(&f.o.Prefix, "prefix", "", "Префикс имён контейнеров инфраструктуры (по умолчанию "+defaultPrefix+")")
	fl.StringVar(&f.o.Home, "home", "", "Папка настроек sudoku (по умолчанию "+displayPath(configs.DefaultHome())+", или $"+configs.HomeEnv+")")
	fl.StringVar(&f.o.BinDir, "bin-dir", "", "Куда поставить бинарник (по умолчанию "+displayPath(setup.DefaultBinDir())+")")
	fl.BoolVar(&f.o.SkipBinary, "no-binary", false, "Не копировать бинарник")
	fl.StringVar(&f.o.MigrateFrom, "migrate-from", "", "Перенести старую установку из этой папки")
	fl.BoolVar(&f.noMigrate, "no-migrate", false, "Не искать и не переносить старую установку")
	fl.BoolVar(&f.noPath, "no-path", false, "Не добавлять папку бинарника в PATH")
	fl.BoolVarP(&f.yes, "yes", "y", false, "Без вопросов: принять значения по умолчанию, перенос и PATH")
	fl.BoolVar(&f.o.OverwriteEnv, "overwrite-env", false, "Пересоздать .env (старый → .env.bak)")
	fl.BoolVar(&f.o.RegenerateCompose, "regenerate-compose", false, "Перегенерировать compose.yaml (старый → compose.yaml.bak)")
	return noConfig(cmd)
}

// displayPath shows paths under the home directory as ~/….
func displayPath(p string) string {
	h, err := os.UserHomeDir()
	if err != nil || p == "" {
		return p
	}
	if rel, err := filepath.Rel(h, p); err == nil && !strings.HasPrefix(rel, "..") {
		if rel == "." {
			return "~"
		}
		return "~" + string(filepath.Separator) + rel
	}
	return p
}

type prompter struct {
	on bool
	in *bufio.Reader
}

func (p *prompter) ask(question, def string) string {
	if !p.on {
		return def
	}
	if def != "" {
		fmt.Printf("%s [%s]: ", question, def)
	} else {
		fmt.Printf("%s: ", question)
	}
	line, _ := p.in.ReadString('\n')
	if v := strings.TrimSpace(line); v != "" {
		return v
	}
	return def
}

func (p *prompter) confirm(question string, def bool) bool {
	if !p.on {
		return def
	}
	hint := "[Y/n]"
	if !def {
		hint = "[y/N]"
	}
	for {
		fmt.Printf("%s %s: ", question, hint)
		line, _ := p.in.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def
		case "y", "yes", "д", "да":
			return true
		case "n", "no", "н", "нет":
			return false
		}
	}
}

// legacyInstall is an old install found on disk, with its infra containers.
type legacyInstall struct {
	dir     string
	file    string
	total   int
	running int
}

// findLegacy looks for an old install (compose.yaml + sudoku-config/ + sudoku .env in one folder):
// in the working directory, next to this binary and behind the compose containers Docker knows.
func findLegacy(ctx context.Context, r *docker.Runner, home string) []legacyInstall {
	byDir := map[string]*legacyInstall{}
	var order []string
	add := func(dir string) *legacyInstall {
		dir = configs.ExpandPath(dir, "")
		if dir == home || !setup.IsLegacy(dir) {
			return nil
		}
		if l, ok := byDir[dir]; ok {
			return l
		}
		l := &legacyInstall{dir: dir, file: filepath.Join(dir, configs.InfraComposeName)}
		byDir[dir] = l
		order = append(order, dir)
		return l
	}
	if wd, err := os.Getwd(); err == nil {
		add(wd)
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		add(filepath.Dir(exe))
	}
	ctx, cancel := context.WithTimeout(ctx, dockerTimeout)
	defer cancel()
	if list, err := r.ListComposeContainers(ctx); err == nil {
		for _, c := range list {
			for _, f := range strings.Split(c.Config.Labels[labelComposeCfg], ",") {
				if filepath.Base(f) != configs.InfraComposeName {
					continue
				}
				if l := add(filepath.Dir(f)); l != nil && filepath.Clean(f) == l.file {
					l.total++
					if c.State.Running {
						l.running++
					}
				}
			}
		}
	}
	out := make([]legacyInstall, 0, len(order))
	for _, d := range order {
		out = append(out, *byDir[d])
	}
	return out
}

func envOf(dir, key string) string {
	return setup.EnvValue(filepath.Join(dir, configs.EnvFileName), key)
}

// askRoot asks for the projects folder until it exists (or the user agrees to create it).
func askRoot(p *prompter, def string) (string, error) {
	for {
		v := p.ask("Папка с вашими проектами", displayPath(def))
		if v == "" {
			if !p.on {
				return "", setup.ErrNoRoot
			}
			clime.WarningLine("Укажите папку, например ~/projects")
			continue
		}
		root := configs.ExpandPath(v, "")
		ok, err := usableRoot(p, root)
		if err != nil || ok {
			return root, err
		}
		if !p.on {
			return "", fmt.Errorf("%w: %s", setup.ErrRootMissing, root)
		}
	}
}

// usableRoot checks that root is a folder, offering to create a missing one.
func usableRoot(p *prompter, root string) (bool, error) {
	st, err := os.Stat(root)
	switch {
	case err == nil && st.IsDir():
		return true, nil
	case err == nil:
		clime.WarningLine(root + " — это файл, а не папка")
		return false, nil
	case !p.on || !p.confirm("Папки "+root+" нет. Создать?", false):
		return false, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return false, fmt.Errorf("mkdir %s: %w", root, err)
	}
	return true, nil
}

func runSetup(ctx context.Context, f *setupFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p := &prompter{on: !f.yes && term.IsTerminal(int(os.Stdin.Fd())), in: bufio.NewReader(os.Stdin)}
	o := f.o
	o.Home = configs.ExpandPath(firstNonEmpty(o.Home, os.Getenv(configs.HomeEnv), configs.DefaultHome()), "")
	o.BinDir = configs.ExpandPath(firstNonEmpty(o.BinDir, setup.DefaultBinDir()), "")

	runner := docker.NewRunner(configs.DefaultDockerPath, configs.DefaultDockerComposePath(), os.Environ())
	legacy, err := chooseLegacy(ctx, p, f, runner, o.Home)
	if err != nil {
		return err
	}
	if err := chooseRootAndPrefix(p, &o, legacy); err != nil {
		return err
	}
	printPlan(o, legacy)
	if !p.confirm("Продолжить?", true) {
		return errSetupCancelled
	}

	if legacy != nil {
		o.MigrateFrom = legacy.dir
		if err := removeLegacyContainers(ctx, runner, legacy); err != nil {
			return err
		}
	}
	steps, err := setup.Run(o)
	for _, s := range steps {
		if s.Done {
			clime.SuccessLine(s.Action + ": " + displayPath(s.Path))
		} else {
			clime.InfoLine(s.Action + ": " + displayPath(s.Path))
		}
	}
	if err != nil {
		return err //nolint:wrapcheck // descriptive
	}
	if legacy != nil {
		restartInfra(ctx, runner, legacy, o.Home)
		cleanupLegacy(p, legacy.dir)
	}
	pathReady := o.SkipBinary || setup.InPath(o.BinDir) || offerPath(p, f, o.BinDir)
	printNext(pathReady, o.BinDir)
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// chooseLegacy picks the old install to move: --migrate-from, or one found and confirmed.
func chooseLegacy(ctx context.Context, p *prompter, f *setupFlags, runner *docker.Runner, home string) (*legacyInstall, error) {
	if f.o.MigrateFrom != "" {
		dir := configs.ExpandPath(f.o.MigrateFrom, "")
		if !setup.IsLegacy(dir) {
			return nil, fmt.Errorf("%s: здесь нет старой установки sudoku (нужны .env, compose.yaml и sudoku-config)", dir)
		}
		for _, l := range findLegacy(ctx, runner, home) {
			if l.dir == dir {
				return &l, nil
			}
		}
		return &legacyInstall{dir: dir, file: filepath.Join(dir, configs.InfraComposeName)}, nil
	}
	if f.noMigrate {
		return nil, nil
	}
	found := findLegacy(ctx, runner, home)
	for i := range found {
		l := found[i]
		clime.InfoLine(fmt.Sprintf("Найдена старая установка: %s (контейнеров инфраструктуры: %d, запущено: %d)", l.dir, l.total, l.running))
		if f.yes || p.confirm("Перенести её в "+displayPath(home)+" вместе с данными (redis, elastic…)?", true) {
			return &l, nil
		}
	}
	if len(found) > 0 && !p.on {
		clime.InfoLine("Перенос не выполнен: запустите `sudoku setup --migrate-from " + found[0].dir + "`")
	}
	return nil, nil
}

func chooseRootAndPrefix(p *prompter, o *setup.Options, legacy *legacyInstall) error {
	def := configs.ExpandPath(o.Root, "")
	for _, d := range []string{o.Home, valueOr(legacy, func(l *legacyInstall) string { return l.dir })} {
		if def == "" && d != "" {
			def = configs.ExpandPath(envOf(d, "ROOT_PROJECTS_FOLDER"), d)
		}
	}
	if o.Root != "" && !p.on {
		if ok, err := usableRoot(p, def); err != nil || !ok {
			return fmt.Errorf("%w: %s", setup.ErrRootMissing, def)
		}
		o.Root = def
	} else {
		root, err := askRoot(p, def)
		if err != nil {
			return err
		}
		o.Root = root
	}
	generate := o.RegenerateCompose || (!pathExists(filepath.Join(o.Home, configs.InfraComposeName)) && legacy == nil)
	if generate && o.Prefix == "" {
		o.Prefix = p.ask("Префикс имён контейнеров инфраструктуры (<префикс>_traefik, <префикс>_redis…)",
			firstNonEmpty(envOf(o.Home, "PREFIX_CONTAINER_NAME"), defaultPrefix))
	}
	return nil
}

func printPlan(o setup.Options, legacy *legacyInstall) {
	fmt.Println()
	clime.InfoLine("Будет установлено:")
	if !o.SkipBinary {
		fmt.Printf("  команда sudoku      %s\n", displayPath(setup.BinaryPath(o.BinDir)))
	}
	fmt.Printf("  настройки (.env)    %s\n", displayPath(filepath.Join(o.Home, configs.EnvFileName)))
	fmt.Printf("  инфраструктура      %s, %s\n",
		displayPath(filepath.Join(o.Home, configs.InfraComposeName)), displayPath(filepath.Join(o.Home, configs.ConfigDirName)))
	fmt.Printf("  папка с проектами   %s\n", displayPath(o.Root))
	if o.Prefix != "" {
		fmt.Printf("  префикс контейнеров %s\n", o.Prefix)
	}
	if legacy != nil {
		fmt.Printf("  перенос из          %s\n", legacy.dir)
	}
	fmt.Println()
}

// removeLegacyContainers removes the old infra containers (not `down`: the external sudoku_network
// that project containers are attached to must survive); bind-mounted data stays on disk.
func removeLegacyContainers(ctx context.Context, runner *docker.Runner, legacy *legacyInstall) error {
	if legacy.total == 0 {
		return nil
	}
	clime.InfoLine("Убираю контейнеры инфраструктуры старой установки (данные и сеть остаются)…")
	cctx, cancel := context.WithTimeout(ctx, dockerTimeout)
	defer cancel()
	if _, err := runner.Compose(cctx, legacy.file, "rm", "--stop", "--force"); err != nil {
		return fmt.Errorf("остановка старой инфраструктуры: %w", err)
	}
	return nil
}

func restartInfra(ctx context.Context, runner *docker.Runner, legacy *legacyInstall, home string) {
	if legacy.running == 0 {
		return
	}
	clime.InfoLine("Поднимаю инфраструктуру из нового места…")
	cctx, cancel := context.WithTimeout(ctx, dockerTimeout)
	defer cancel()
	if _, err := runner.Compose(cctx, filepath.Join(home, configs.InfraComposeName), "up", "-d"); err != nil {
		clime.WarningLine("не удалось поднять: " + err.Error() + " — запустите `sudoku start infra`")
		return
	}
	clime.SuccessLine("инфраструктура запущена")
}

func printNext(pathReady bool, binDir string) {
	fmt.Println()
	clime.SuccessLine("Готово.")
	exe := "sudoku"
	if !pathReady {
		exe = setup.BinaryPath(binDir)
	}
	fmt.Printf("  Проверка:  %s doctor\n  Запуск:    %s start\n  Админка:   %s serve  →  http://%s\n", exe, exe, exe, configs.DefaultAdminAddr)
	if adminRunning() {
		clime.WarningLine("Админка уже запущена (" + configs.DefaultAdminAddr + ") — перезапустите её, чтобы она подхватила новые пути")
	}
}

func valueOr(l *legacyInstall, get func(*legacyInstall) string) string {
	if l == nil {
		return ""
	}
	return get(l)
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func offerPath(p *prompter, f *setupFlags, binDir string) bool {
	file, line := setup.PathHint(binDir)
	if f.noPath || (!p.on && !f.yes) {
		clime.InfoLine(displayPath(binDir) + " нет в PATH. Добавьте строку в " + displayPath(file) + ": " + line)
		return false
	}
	if !p.confirm("Добавить "+displayPath(binDir)+" в PATH ("+displayPath(file)+"), чтобы команда sudoku работала из любой папки?", true) {
		return false
	}
	written, err := setup.AddToPath(binDir)
	if err != nil {
		clime.WarningLine(err.Error())
		return false
	}
	clime.SuccessLine("PATH: " + displayPath(written) + " — откройте новый терминал (или `source " + displayPath(written) + "`)")
	return true
}

// cleanupLegacy offers to delete what an old install leaves behind (its binary and .env copies)
// when nothing else is in the folder.
func cleanupLegacy(p *prompter, dir string) {
	known, other := setup.Leftovers(dir)
	if len(other) > 0 {
		names := make([]string, 0, len(other))
		for _, o := range other {
			names = append(names, filepath.Base(o))
		}
		slices.Sort(names)
		clime.InfoLine("В старой папке " + dir + " остались другие файлы (" + strings.Join(names, ", ") + ") — не трогаю")
		return
	}
	if !p.on || !p.confirm("Удалить старую папку "+dir+" (в ней остались только старый бинарник и копии .env)?", false) {
		clime.InfoLine("Старую папку " + dir + " можно удалить")
		return
	}
	for _, k := range known {
		_ = os.Remove(k)
	}
	if err := os.Remove(dir); err != nil {
		clime.WarningLine("не удалось удалить " + dir + ": " + err.Error())
		return
	}
	clime.SuccessLine("удалена " + dir)
}

func adminRunning() bool {
	d := net.Dialer{Timeout: 300 * time.Millisecond}
	c, err := d.Dial("tcp", configs.DefaultAdminAddr)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
