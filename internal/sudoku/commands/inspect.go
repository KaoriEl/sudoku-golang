package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sudoku-golang/internal/admin"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/engine"
	"sudoku-golang/internal/metrics"
	"sudoku-golang/internal/service"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/alperdrsnn/clime"
	"github.com/spf13/cobra"
)

const (
	statusTimeout = 60 * time.Second
	shellCmd      = "command -v bash >/dev/null 2>&1 && exec bash || exec sh"
)

var errAmbiguous = errors.New("цель должна указывать на один сервис")

func (a *app) snapshot(ctx context.Context) (admin.Snapshot, error) {
	runner := docker.NewRunner(a.cfg.DockerPath, a.cfg.DockerComposePath, os.Environ())
	eng := engine.New(engine.ResolveEndpoint(ctx, runner.DockerArgv(), runner.Env()))
	svc := admin.NewService(a.log, a.cfg, runner, metrics.NewCollector(a.log, eng))
	return svc.Snapshot(ctx) //nolint:wrapcheck // already descriptive
}

var statusColor = map[admin.Status]*clime.Color{
	admin.StatusRunning:    clime.GreenColor,
	admin.StatusStarting:   clime.CyanColor,
	admin.StatusRestarting: clime.CyanColor,
	admin.StatusUnhealthy:  clime.YellowColor,
	admin.StatusPaused:     clime.YellowColor,
	admin.StatusError:      clime.RedColor,
	admin.StatusStopped:    clime.BrightBlackColor,
}

var statusRu = map[admin.Status]string{
	admin.StatusRunning: "работает", admin.StatusStarting: "запуск", admin.StatusRestarting: "перезапуск",
	admin.StatusUnhealthy: "нездоров", admin.StatusPaused: "пауза", admin.StatusError: "ошибка", admin.StatusStopped: "остановлен",
}

func since(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil || t.Year() < 2000 {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "с"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "м"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "ч"
	}
	return strconv.Itoa(int(d.Hours()/24)) + "д"
}

func ports(c admin.Container) string {
	var out []string
	for _, p := range c.Ports {
		if p.HostPort > 0 {
			out = append(out, strconv.Itoa(p.HostPort)+"→"+strconv.Itoa(p.ContainerPort))
		}
	}
	if len(out) == 0 {
		return "—"
	}
	return strings.Join(out, " ")
}

// filterSnapshot keeps the containers matched by targets (resolved against the snapshot's files).
func filterSnapshot(snap admin.Snapshot, root string, targets []string) ([]admin.Container, error) {
	if len(targets) == 0 {
		return snap.Containers, nil
	}
	projects := make([]service.Project, 0, len(snap.ComposeFiles))
	files := make([]string, 0, len(snap.ComposeFiles))
	for _, f := range snap.ComposeFiles {
		files = append(files, f.Path)
	}
	projects = append(projects, service.ProjectsFrom(root, files)...)
	for i := range projects {
		projects[i].Services = snap.ComposeFiles[i].Services
	}
	entries, err := service.Resolve(projects, targets)
	if err != nil {
		return nil, err //nolint:wrapcheck // already descriptive
	}
	want := map[string]map[string]bool{}
	for _, e := range entries {
		set := map[string]bool{}
		for _, s := range e.Services {
			set[s] = true
		}
		want[e.Project.File] = set
	}
	var out []admin.Container
	for _, c := range snap.Containers {
		if set, ok := want[c.ComposeFile]; ok && (len(set) == 0 || set[c.Service]) {
			out = append(out, c)
		}
	}
	return out, nil
}

func commandStatus(a *app) *cobra.Command {
	var asJSON, all bool
	cmd := &cobra.Command{
		Use:               "status [цели...]",
		Aliases:           []string{"ps"},
		Short:             "Состояние контейнеров (как в админке)",
		ValidArgsFunction: completeTargets(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), statusTimeout)
			defer cancel()
			snap, err := a.snapshot(ctx)
			if err != nil {
				return err
			}
			list, err := filterSnapshot(snap, a.cfg.RootProjectsFolder, args)
			if err != nil {
				return err
			}
			if !all {
				kept := list[:0]
				for _, c := range list {
					if c.Managed {
						kept = append(kept, c)
					}
				}
				list = kept
			}
			if asJSON {
				return printJSON(cmd.OutOrStdout(), list)
			}
			return printStatus(cmd.OutOrStdout(), snap, list)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Вывести в JSON")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Показать и чужие compose-контейнеры")
	return cmd
}

func printStatus(w io.Writer, snap admin.Snapshot, list []admin.Container) error {
	rel := map[string]string{}
	for _, f := range snap.ComposeFiles {
		rel[f.Path] = f.Rel
	}
	sort.SliceStable(list, func(i, j int) bool { return rel[list[i].ComposeFile] < rel[list[j].ComposeFile] })
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ФАЙЛ\tСЕРВИС\tСОСТОЯНИЕ\tВРЕМЯ\tПОРТЫ\tКОНТЕЙНЕР")
	running := 0
	for _, c := range list {
		file := rel[c.ComposeFile]
		if file == "" {
			file = c.Project
		}
		st := statusRu[c.Status]
		if c.Disabled {
			st += " (откл.)"
		}
		col := statusColor[c.Status]
		if col == nil {
			col = clime.WhiteColor
		}
		if c.Status == admin.StatusRunning {
			running++
		}
		at := c.StartedAt
		if c.Status == admin.StatusStopped || c.Status == admin.StatusError {
			at = c.FinishedAt
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", file, c.Service, col.Sprint(fmt.Sprintf("%-18s", st)), since(at), ports(c), c.Name)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("print: %w", err)
	}
	_, err := fmt.Fprintf(w, "\nработает %d из %d\n", running, len(list))
	return err //nolint:wrapcheck // stdout
}

// resolveOne turns targets into compose file → services.
func (a *app) resolve(targets, sets []string) (*service.Composer, error) {
	if len(targets) == 0 && len(sets) == 0 {
		return nil, errNoTarget
	}
	return service.NewComposer(a.log, a.cfg, service.Options{Targets: targets, Sets: sets}) //nolint:wrapcheck // descriptive
}

// passthrough runs an interactive child with the terminal attached; Ctrl+C goes to the child (same
// console / process group) instead of killing sudoku first.
func passthrough(cmd *exec.Cmd) error {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)
	return cmd.Run() //nolint:wrapcheck // exit status is the result
}

func exitCode(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode())
	}
	return err
}

func commandLogs(a *app) *cobra.Command {
	var follow, timestamps bool
	var tail, sinceFlag string
	var sets []string
	cmd := &cobra.Command{
		Use:               "logs <цели...>",
		Short:             "Логи сервисов (docker compose logs)",
		Example:           "  sudoku logs api -f\n  sudoku logs infra/redis --tail 50\n  sudoku logs --set backend -f",
		ValidArgsFunction: completeTargets(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.resolve(args, sets)
			if err != nil {
				return err
			}
			extra := []string{"logs", "--tail", tail}
			if follow {
				extra = append(extra, "--follow")
			}
			if timestamps {
				extra = append(extra, "--timestamps")
			}
			if sinceFlag != "" {
				extra = append(extra, "--since", sinceFlag)
			}
			signal.Ignore(os.Interrupt)
			defer signal.Reset(os.Interrupt)
			var wg sync.WaitGroup
			errs := make([]error, len(c.Entries()))
			for i, e := range c.Entries() {
				wg.Add(1)
				go func(i int, e service.Entry) {
					defer wg.Done()
					full := append(append([]string{"--file", e.Project.File}, extra...), e.Services...)
					run, err := c.Runner().Command(cmd.Context(), c.Runner().ComposeArgv(), full...)
					if err != nil {
						errs[i] = err
						return
					}
					run.Stdout, run.Stderr = os.Stdout, os.Stderr
					errs[i] = run.Run()
				}(i, e)
			}
			wg.Wait()
			return errors.Join(errs...)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Следить за новыми строками")
	cmd.Flags().BoolVarP(&timestamps, "timestamps", "t", false, "Показывать время")
	cmd.Flags().StringVarP(&tail, "tail", "n", "200", "Сколько последних строк (all — все)")
	cmd.Flags().StringVar(&sinceFlag, "since", "", "С какого момента (10m, 2h, 2024-01-02T15:04:05)")
	cmd.Flags().StringSliceVar(&sets, "set", nil, "Набор из sets.yaml")
	return cmd
}

func commandExec(a *app) *cobra.Command {
	var user string
	var noTTY bool
	cmd := &cobra.Command{
		Use:               "exec <сервис> [-- команда...]",
		Short:             "Открыть shell или выполнить команду в сервисе",
		Example:           "  sudoku exec api/app\n  sudoku exec redis -- redis-cli info\n  sudoku exec billing/db -- psql -U postgres",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeTargets(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, command := args[0], args[1:]
			if dash := cmd.ArgsLenAtDash(); dash >= 1 {
				target, command = args[0], args[dash:]
				if dash > 1 {
					return errAmbiguous
				}
			}
			c, err := a.resolve([]string{target}, nil)
			if err != nil {
				return err
			}
			entries := c.Entries()
			if len(entries) != 1 {
				return fmt.Errorf("%w: %s совпадает с %d файлами (%s)", errAmbiguous, target, len(entries), c.Describe())
			}
			e := entries[0]
			services := e.Services
			if services == nil {
				services = c.Services(e.Project.File)
			}
			if len(services) != 1 {
				return fmt.Errorf("%w: уточните %s/<сервис> (%s)", errAmbiguous, e.Project.Name, strings.Join(services, ", "))
			}
			full := []string{"--file", e.Project.File, "exec"}
			if user != "" {
				full = append(full, "--user", user)
			}
			if noTTY {
				full = append(full, "-T")
			}
			full = append(full, services[0])
			if len(command) == 0 {
				command = []string{"sh", "-c", shellCmd}
			}
			run, err := c.Runner().Command(cmd.Context(), c.Runner().ComposeArgv(), append(full, command...)...)
			if err != nil {
				return err //nolint:wrapcheck // ErrEmptyCommand
			}
			return exitCode(passthrough(run))
		},
	}
	cmd.Flags().StringVarP(&user, "user", "u", "", "Пользователь внутри контейнера")
	cmd.Flags().BoolVarP(&noTTY, "no-tty", "T", false, "Без TTY (для пайпов и скриптов)")
	return cmd
}

func commandSets(a *app) *cobra.Command {
	root := &cobra.Command{Use: "sets", Short: "Наборы проектов (sudoku-config/sets.yaml)"}
	list := &cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "Показать наборы", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sets, err := service.NewSetsStore(a.cfg.RootProjectsFolder).List()
			if err != nil {
				return err //nolint:wrapcheck // descriptive
			}
			if len(sets) == 0 {
				clime.InfoLine("Наборов нет. Добавить: sudoku sets add backend infra api billing")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "НАБОР\tЦЕЛИ\tОПИСАНИЕ")
			for _, s := range sets {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, strings.Join(s.Targets, " "), s.Description)
			}
			return tw.Flush()
		},
	}
	var desc string
	add := &cobra.Command{
		Use: "add <имя> <цели...>", Short: "Создать или заменить набор", Args: cobra.MinimumNArgs(2),
		ValidArgsFunction: completeTargets(a),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := service.NewComposer(a.log, a.cfg, service.Options{Targets: args[1:]})
			if err != nil {
				return err //nolint:wrapcheck // validates the targets
			}
			set, err := service.NewSetsStore(a.cfg.RootProjectsFolder).Upsert(service.Set{Name: args[0], Description: desc, Targets: args[1:]})
			if err != nil {
				return err //nolint:wrapcheck // descriptive
			}
			clime.SuccessLine(fmt.Sprintf("Набор %s: %s", set.Name, c.Describe()))
			return nil
		},
	}
	add.Flags().StringVarP(&desc, "description", "d", "", "Описание")
	rm := &cobra.Command{
		Use: "rm <имя>", Aliases: []string{"delete"}, Short: "Удалить набор", Args: cobra.ExactArgs(1),
		ValidArgsFunction: completeSets(a),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := service.NewSetsStore(a.cfg.RootProjectsFolder).Delete(args[0]); err != nil {
				return err //nolint:wrapcheck // descriptive
			}
			clime.SuccessLine("Набор удалён: " + args[0])
			return nil
		},
	}
	root.AddCommand(list, add, rm)
	return root
}
