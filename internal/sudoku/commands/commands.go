package commands

import (
	"fmt"
	"log/slog"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/logger"

	"github.com/alperdrsnn/clime"
	"github.com/spf13/cobra"
)

const (
	// Логи
	logEnabledMsg  = "«%s» command executed with logging enabled"
	logDisabledMsg = "«%s» command executed with logging disabled"
	logFlagDesc    = "Turn on detailed log"

	// Старт/финиш команд
	startCmdMsg  = "Starting command «%s»"
	finishCmdMsg = "Finish command «%s»..."

	// Описания команд
	descBuild        = "Собирает все контейнеры"
	descStart        = "Запускает собранные контейнеры"
	descStop         = "Останавливает все контейнеры"
	descForceBuild   = "Принудительно собирает контейнеры"
	descDown         = "Останавливает и удаляет все контейнеры"
	descRestart      = "Перезапускает все контейнеры"
	descRebuild      = "Останавливает, собирает и запускает все контейнеры"
	descForceRebuild = "Принудительно пересобираем контейнеры"
)

func logCommandRun(log *slog.Logger, cmd *cobra.Command, logEnabled bool) *slog.Logger {
	msg := logDisabledMsg
	if logEnabled {
		msg = logEnabledMsg
	}

	log = logger.NewLogger(logEnabled)
	log.Info(fmt.Sprintf(msg, cmd.Use))
	return log
}

func GetCommands(log *slog.Logger, config *configs.Config) []*cobra.Command {
	return []*cobra.Command{
		commandBuild(log, config),
		commandStart(log, config),
		commandStop(log, config),
		commandForceBuild(log, config),
		commandDown(log, config),
		commandRestart(log, config),
		commandRebuild(log, config),
		commandForceRebuild(log, config),
	}
}

func commandBuild(log *slog.Logger, config *configs.Config) *cobra.Command {
	var logEnabled bool

	cmd := &cobra.Command{
		Use:   "build",
		Short: descBuild,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			clime.InfoLine(fmt.Sprintf(startCmdMsg, cmd.Use))
			log = logCommandRun(log, cmd, logEnabled)
			build(log, config)
			clime.InfoLine(fmt.Sprintf(finishCmdMsg, cmd.Use))
		},
	}

	cmd.Flags().BoolVar(&logEnabled, "log", false, logFlagDesc)
	return cmd
}

func commandStart(log *slog.Logger, config *configs.Config) *cobra.Command {
	var logEnabled bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: descStart,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			clime.InfoLine(fmt.Sprintf(startCmdMsg, cmd.Use))
			log = logCommandRun(log, cmd, logEnabled)
			start(log, config)
			clime.InfoLine(fmt.Sprintf(finishCmdMsg, cmd.Use))
		},
	}

	cmd.Flags().BoolVar(&logEnabled, "log", false, logFlagDesc)
	return cmd
}

func commandStop(log *slog.Logger, config *configs.Config) *cobra.Command {
	var logEnabled bool

	cmd := &cobra.Command{
		Use:   "stop",
		Short: descStop,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			clime.InfoLine(fmt.Sprintf(startCmdMsg, cmd.Use))
			log = logCommandRun(log, cmd, logEnabled)
			stop(log, config)
			clime.InfoLine(fmt.Sprintf(finishCmdMsg, cmd.Use))
		},
	}

	cmd.Flags().BoolVar(&logEnabled, "log", false, logFlagDesc)
	return cmd
}

func commandForceBuild(log *slog.Logger, config *configs.Config) *cobra.Command {
	var logEnabled bool

	cmd := &cobra.Command{
		Use:   "force-build",
		Short: descForceBuild,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			clime.InfoLine(fmt.Sprintf(startCmdMsg, cmd.Use))
			log = logCommandRun(log, cmd, logEnabled)
			forceBuild(log, config)
			clime.InfoLine(fmt.Sprintf(finishCmdMsg, cmd.Use))
		},
	}

	cmd.Flags().BoolVar(&logEnabled, "log", false, logFlagDesc)
	return cmd
}

func commandDown(log *slog.Logger, config *configs.Config) *cobra.Command {
	var logEnabled bool

	cmd := &cobra.Command{
		Use:   "down",
		Short: descDown,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			clime.InfoLine(fmt.Sprintf(startCmdMsg, cmd.Use))
			log = logCommandRun(log, cmd, logEnabled)
			down(log, config)
			clime.InfoLine(fmt.Sprintf(finishCmdMsg, cmd.Use))
		},
	}

	cmd.Flags().BoolVar(&logEnabled, "log", false, logFlagDesc)
	return cmd
}

func commandRestart(log *slog.Logger, config *configs.Config) *cobra.Command {
	var logEnabled bool

	cmd := &cobra.Command{
		Use:   "restart",
		Short: descRestart,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			clime.InfoLine(fmt.Sprintf(startCmdMsg, cmd.Use))
			log = logCommandRun(log, cmd, logEnabled)
			restart(log, config)
			clime.InfoLine(fmt.Sprintf(finishCmdMsg, cmd.Use))
		},
	}

	cmd.Flags().BoolVar(&logEnabled, "log", false, logFlagDesc)
	return cmd
}

func commandRebuild(log *slog.Logger, config *configs.Config) *cobra.Command {
	var logEnabled bool

	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: descRebuild,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			clime.InfoLine(fmt.Sprintf(startCmdMsg, cmd.Use))
			log = logCommandRun(log, cmd, logEnabled)
			rebuild(log, config)
			clime.InfoLine(fmt.Sprintf(finishCmdMsg, cmd.Use))
		},
	}

	cmd.Flags().BoolVar(&logEnabled, "log", false, logFlagDesc)
	return cmd
}

func commandForceRebuild(log *slog.Logger, config *configs.Config) *cobra.Command {
	var logEnabled bool

	cmd := &cobra.Command{
		Use:   "force-rebuild",
		Short: descForceRebuild,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			clime.InfoLine(fmt.Sprintf(startCmdMsg, cmd.Use))
			log = logCommandRun(log, cmd, logEnabled)
			forceRebuild(log, config)
			clime.InfoLine(fmt.Sprintf(finishCmdMsg, cmd.Use))
		},
	}

	cmd.Flags().BoolVar(&logEnabled, "log", false, logFlagDesc)
	return cmd
}
