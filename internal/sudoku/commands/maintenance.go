package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/doctor"
	"sudoku-golang/internal/engine"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/selfupdate"
	"sudoku-golang/internal/setup"
	"sudoku-golang/internal/version"
	"time"

	"github.com/alperdrsnn/clime"
	"github.com/spf13/cobra"
)

const (
	doctorTimeout = 3 * time.Minute
	updateTimeout = 10 * time.Minute
)

var levelMark = map[doctor.Level]string{
	doctor.OK:   clime.GreenColor.Sprint("✓"),
	doctor.Info: clime.CyanColor.Sprint("i"),
	doctor.Warn: clime.YellowColor.Sprint("!"),
	doctor.Fail: clime.RedColor.Sprint("✗"),
}

func commandDoctor(a *app) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Проверить окружение: docker, compose, сокет, проекты, порты, права",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), doctorTimeout)
			defer cancel()
			runner := docker.NewRunner(a.cfg.DockerPath, a.cfg.DockerComposePath, os.Environ())
			eng := engine.New(engine.ResolveEndpoint(ctx, runner.DockerArgv(), runner.Env()))
			if !asJSON {
				banner("doctor")
			}
			rep := doctor.Run(ctx, doctor.Env{Cfg: a.cfg, Runner: runner, Engine: eng})
			if asJSON {
				if err := printJSON(cmd.OutOrStdout(), rep); err != nil {
					return err
				}
			} else {
				group := ""
				for _, ch := range rep.Checks {
					if ch.Group != group {
						group = ch.Group
						_, _ = fmt.Fprintln(cmd.OutOrStdout(), "\n"+clime.BoldColor.Sprint(group))
					}
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s %s — %s\n", levelMark[ch.Level], ch.Title, ch.Detail)
					if ch.Hint != "" && ch.Level != doctor.OK {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "      %s\n", clime.DimColor.Sprint("→ "+ch.Hint))
					}
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nошибок: %d, предупреждений: %d\n", rep.Fails, rep.Warns)
			}
			if rep.Fails > 0 {
				return doctor.ErrFailed
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Вывести в JSON")
	return cmd
}

func commandSetup() *cobra.Command {
	var o setup.Options
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Развернуть sudoku в ROOT_PROJECTS_FOLDER (бинарник, .env, sudoku-config, compose.yaml)",
		Long: "Кроссплатформенная замена деплой-части build.sh: копирует этот бинарник и .env в корень проектов,\n" +
			"раскладывает sudoku-config (из репозитория или встроенный) и генерирует compose.yaml с префиксом\n" +
			"PREFIX_CONTAINER_NAME. Существующие файлы не трогает без --overwrite-env / --regenerate-compose.",
		Example: "  sudoku setup --root ~/projects --prefix dev\n  sudoku setup --regenerate-compose",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			banner("setup")
			if o.Root == "" {
				if cfg, err := configs.MustLoad(); err == nil && os.Getenv("ROOT_PROJECTS_FOLDER") != "" {
					o.Root = cfg.RootProjectsFolder
				}
			}
			steps, err := setup.Run(o)
			for _, s := range steps {
				if s.Done {
					clime.SuccessLine(s.Action + ": " + s.Path)
				} else {
					clime.InfoLine(s.Action + ": " + s.Path)
				}
			}
			if err != nil {
				return err //nolint:wrapcheck // descriptive
			}
			root, _ := filepath.Abs(o.Root)
			clime.SuccessLine("Готово. Дальше: cd " + root + " && ./sudoku doctor && ./sudoku start")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.Root, "root", "", "Папка проектов (по умолчанию ROOT_PROJECTS_FOLDER из .env)")
	f.StringVar(&o.Prefix, "prefix", "", "Префикс имён контейнеров (по умолчанию PREFIX_CONTAINER_NAME)")
	f.BoolVar(&o.OverwriteEnv, "overwrite-env", false, "Перезаписать .env в корне (старый → .env.bak)")
	f.BoolVar(&o.RegenerateCompose, "regenerate-compose", false, "Перегенерировать compose.yaml (старый → compose.yaml.bak)")
	f.BoolVar(&o.SkipBinary, "no-binary", false, "Не копировать бинарник")
	return noConfig(cmd)
}

func commandUpdate(_ *app) *cobra.Command {
	var check, force bool
	var repo string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Обновить sudoku до последнего релиза",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), updateTimeout)
			defer cancel()
			selfupdate.CleanupOld()
			u := selfupdate.New(repo)
			rel, err := u.Latest(ctx)
			if err != nil {
				return fmt.Errorf("последний релиз %s: %w", repo, err)
			}
			cur := version.Get()
			clime.InfoLine(fmt.Sprintf("Текущая версия: %s, последняя: %s", cur.Version, rel.Tag))
			newer := selfupdate.Newer(cur.Version, rel.Tag)
			if !newer && !force {
				if !version.IsRelease() {
					clime.WarningLine("Это не релизная сборка — версию сравнить нельзя. Обновить принудительно: sudoku update --force")
				} else {
					clime.SuccessLine("Обновление не требуется")
				}
				return nil
			}
			if check {
				clime.InfoLine("Доступно обновление: " + rel.URL)
				return nil
			}
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("executable: %w", err)
			}
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				exe = resolved
			}
			if err := u.Apply(ctx, rel, exe); err != nil {
				if errors.Is(err, os.ErrPermission) {
					return fmt.Errorf("нет прав на запись %s — запустите с правами владельца файла: %w", exe, err)
				}
				return err //nolint:wrapcheck // descriptive
			}
			clime.SuccessLine("Обновлено до " + rel.Tag + ": " + exe)
			clime.InfoLine("Запущенную админку перезапустите (Настройки → сохранить конфиг, или перезапуск службы)")
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Только проверить, не обновлять")
	cmd.Flags().BoolVar(&force, "force", false, "Обновить, даже если версия не новее (или это dev-сборка)")
	cmd.Flags().StringVar(&repo, "repo", version.Repo, "GitHub-репозиторий owner/name с релизами")
	return noConfig(cmd)
}
