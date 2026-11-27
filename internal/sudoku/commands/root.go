// Package commands is the sudoku CLI (cobra).
package commands

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/logger"
	"sudoku-golang/internal/version"

	"github.com/alperdrsnn/clime"
	"github.com/spf13/cobra"
)

const (
	annNoConfig = "sudoku/no-config"

	groupActions = "actions"
	groupInspect = "inspect"
	groupAdmin   = "admin"
)

var errNoTarget = errors.New("укажите цель")

// app carries what every command needs; config is loaded lazily (after --chdir) so commands such as
// `version` and `completion` work without a .env.
type app struct {
	log        *slog.Logger
	cfg        *configs.Config
	debug      bool
	logEnabled bool
	chdir      string
}

func (a *app) load() error {
	if a.chdir != "" {
		if err := os.Chdir(a.chdir); err != nil {
			return fmt.Errorf("--chdir %s: %w", a.chdir, err)
		}
	}
	a.log = logger.NewLogger(a.logEnabled)
	cfg, err := configs.MustLoad()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	a.cfg = cfg
	return nil
}

func noConfig(cmd *cobra.Command) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[annNoConfig] = "1"
	return cmd
}

func needsConfig(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[annNoConfig] != "" {
			return false
		}
		if c.Name() == cobra.ShellCompRequestCmd || c.Name() == cobra.ShellCompNoDescRequestCmd || c.Name() == "completion" {
			return false
		}
	}
	return true
}

// Root returns the whole command tree.
func Root() *cobra.Command {
	a := &app{}
	root := &cobra.Command{
		Use:   "sudoku",
		Short: "Оркестратор локальной Docker Compose инфраструктуры",
		Long: "sudoku находит compose-файлы в ROOT_PROJECTS_FOLDER и управляет ими разом или выборочно.\n" +
			"Цели: проект (папка), infra, сервис, проект/сервис, шаблоны api-*; наборы — --set <имя>.",
		Version:       version.Get().String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if !needsConfig(cmd) {
				a.log = logger.NewLogger(a.logEnabled)
				return nil
			}
			return a.load()
		},
	}
	root.SetVersionTemplate("sudoku {{.Version}}\n")
	pf := root.PersistentFlags()
	pf.BoolVar(&a.debug, "debug", false, "Печатать каждую выполняемую команду вместо прогресс-бара")
	pf.BoolVar(&a.logEnabled, "log", false, "Подробный JSON-лог (иначе только ошибки)")
	pf.StringVarP(&a.chdir, "chdir", "C", "", "Перейти в эту папку перед чтением .env")

	root.AddGroup(
		&cobra.Group{ID: groupActions, Title: "Действия:"},
		&cobra.Group{ID: groupInspect, Title: "Просмотр:"},
		&cobra.Group{ID: groupAdmin, Title: "Админка и обслуживание:"},
	)
	for _, c := range actionCommands(a) {
		c.GroupID = groupActions
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{commandStatus(a), commandLogs(a), commandExec(a), commandSets(a)} {
		c.GroupID = groupInspect
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{commandServe(a), commandDoctor(a), commandSetup(), commandUpdate(a), commandVersion()} {
		c.GroupID = groupAdmin
		root.AddCommand(c)
	}
	root.CompletionOptions.HiddenDefaultCmd = false
	return root
}

func banner(title string) {
	clime.Header("🧩  sudoku · " + title)
}

func commandVersion() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Версия sudoku",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := version.Get()
			if asJSON {
				return printJSON(cmd.OutOrStdout(), info)
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "sudoku %s\ngo %s\n", info, info.Go)
			return err //nolint:wrapcheck // stdout
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Вывести в JSON")
	return noConfig(cmd)
}
