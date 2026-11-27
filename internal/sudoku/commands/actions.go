package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/logger"
	"sudoku-golang/internal/service"

	"github.com/alperdrsnn/clime"
	"github.com/spf13/cobra"
)

type actionFlags struct {
	sets        []string
	wait        bool
	noWait      bool
	noPreflight bool
}

func (f *actionFlags) options(cmd *cobra.Command, a *app, targets []string, force bool) service.Options {
	o := service.Options{ForceRebuild: force, Debug: a.debug, Targets: targets, Sets: f.sets, NoPreflight: f.noPreflight}
	switch {
	case cmd.Flags().Changed("no-wait") && f.noWait:
		v := false
		o.Wait = &v
	case cmd.Flags().Changed("wait") && f.wait:
		v := true
		o.Wait = &v
	}
	return o
}

type action struct {
	use, short string
	force      bool
	starts     bool
	run        func(c *service.Composer) error
}

func actionCommands(a *app) []*cobra.Command {
	actions := []action{
		{use: "build", short: "Собрать контейнеры", run: (*service.Composer).Build},
		{use: "force-build", short: "Собрать без кэша", force: true, run: (*service.Composer).Build},
		{use: "start", short: "Запустить контейнеры", starts: true, run: (*service.Composer).Start},
		{use: "stop", short: "Остановить контейнеры", run: (*service.Composer).Stop},
		{use: "down", short: "Остановить и удалить контейнеры", run: (*service.Composer).Down},
		{use: "restart", short: "Перезапустить (stop → start)", starts: true, run: func(c *service.Composer) error {
			return firstErr(c.Stop(), c.Start())
		}},
		{use: "rebuild", short: "Пересобрать (stop → build → start)", starts: true, run: func(c *service.Composer) error {
			return firstErr(c.Stop(), c.Build(), c.Start())
		}},
		{use: "force-rebuild", short: "Пересобрать без кэша", force: true, starts: true, run: func(c *service.Composer) error {
			return firstErr(c.Stop(), c.Build(), c.Start())
		}},
	}
	out := make([]*cobra.Command, 0, len(actions))
	for _, ac := range actions {
		out = append(out, actionCommand(a, ac))
	}
	return out
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func actionCommand(a *app, ac action) *cobra.Command {
	var f actionFlags
	cmd := &cobra.Command{
		Use:               ac.use + " [цели...]",
		Short:             ac.short,
		Long:              ac.short + ".\nБез целей — все проекты. Цели: проект, infra, сервис, проект/сервис, шаблоны (api-*, */db).",
		Example:           "  sudoku " + ac.use + "\n  sudoku " + ac.use + " api billing/worker\n  sudoku " + ac.use + " --set backend",
		ValidArgsFunction: completeTargets(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			banner(ac.use)
			c, err := service.NewComposer(a.log, a.cfg, f.options(cmd, a, args, ac.force))
			if err != nil {
				return err //nolint:wrapcheck // already descriptive
			}
			if len(args) > 0 || len(f.sets) > 0 {
				clime.InfoLine("Цели: " + c.Describe())
			}
			if err := ac.run(c); err != nil {
				return err
			}
			clime.SuccessLine("Готово: " + ac.use)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&f.sets, "set", nil, "Набор из sudoku-config/sets.yaml (можно несколько)")
	_ = cmd.RegisterFlagCompletionFunc("set", completeSets(a))
	if ac.starts {
		cmd.Flags().BoolVar(&f.wait, "wait", false, "Ждать healthcheck инфраструктуры перед проектами")
		cmd.Flags().BoolVar(&f.noWait, "no-wait", false, "Не ждать healthcheck инфраструктуры")
		cmd.Flags().BoolVar(&f.noPreflight, "no-preflight", false, "Не проверять занятые порты перед запуском")
		cmd.MarkFlagsMutuallyExclusive("wait", "no-wait")
	}
	return cmd
}

// completionApp loads the config quietly for shell completion (PersistentPreRun does not run there).
func completionApp(a *app) bool {
	if a.cfg != nil {
		return true
	}
	cfg, err := configs.MustLoad()
	if err != nil {
		return false
	}
	a.cfg, a.log = cfg, logger.NewLogger(false)
	return true
}

// completeTargets offers project names (and infra); after "project/" it lists that project's services.
func completeTargets(a *app) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if !completionApp(a) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		files, err := service.DiscoverComposeFiles(a.cfg)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		projects := service.LayoutOf(a.cfg).Projects(files)
		if proj, _, ok := strings.Cut(toComplete, "/"); ok {
			c, err := service.NewComposer(a.log, a.cfg, service.Options{})
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			var out []string
			for _, p := range projects {
				if p.Name == proj {
					for _, s := range c.Services(p.File) {
						out = append(out, proj+"/"+s)
					}
				}
			}
			return out, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]string, 0, len(projects))
		for _, p := range projects {
			desc := p.Rel
			out = append(out, p.Name+"\t"+desc)
		}
		sort.Strings(out)
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

func completeSets(a *app) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		if !completionApp(a) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		sets, err := service.NewSetsStore(service.LayoutOf(a.cfg)).List()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]string, 0, len(sets))
		for _, s := range sets {
			out = append(out, s.Name+"\t"+s.Description)
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}
