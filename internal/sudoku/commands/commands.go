package commands

import (
	"github.com/spf13/cobra"
)

func GetCommands() []*cobra.Command {
	cmds := []*cobra.Command{
		commandBuild(),
		commandStart(),
		commandStop(),
		commandForceBuild(),
		commandDown(),
		commandRestart(),
		commandRebuild(),
		commandForceRebuild(),
	}
	return cmds
}

func commandBuild() *cobra.Command {
	return &cobra.Command{
		Use:   "build",
		Short: "Собирает все контейнеры",
		Long:  ``,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			build()
		},
	}
}

func commandStop() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Останавливает все контейнеры",
		Long:  ``,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			stop()
		},
	}
}

func commandStart() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "запускает собранные контейнеры",
		Long:  ``,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			start()
		},
	}
}

func commandForceBuild() *cobra.Command {
	return &cobra.Command{
		Use:   "force-build",
		Short: "принудительно собирает контейнеры",
		Long:  ``,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			forceBuild()
		},
	}
}

func commandDown() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Останавливает и удаляет все контейнеры",
		Long:  ``,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			down()
		},
	}
}

func commandRestart() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Перезапускает все контейнеры",
		Long:  ``,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			restart()
		},
	}
}

func commandRebuild() *cobra.Command {
	return &cobra.Command{
		Use:   "rebuild",
		Short: "Останавливает, собирает и запускает все контейнеры",
		Long:  ``,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			rebuild()
		},
	}
}

func commandForceRebuild() *cobra.Command {
	return &cobra.Command{
		Use:   "force-rebuild",
		Short: "принудительно пересобирает контейнеры",
		Long:  ``,
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			forceRebuild()
		},
	}
}
