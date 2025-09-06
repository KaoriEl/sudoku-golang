package sudoku

import (
	"fmt"
	"log/slog"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/sudoku/commands"

	"github.com/alperdrsnn/clime"
	"github.com/spf13/cobra"
)

func Run(log *slog.Logger, cfg *configs.Config) error {
	clime.InfoLine("Starting application...")
	rootCmd := &cobra.Command{Use: "run"}
	cmds := commands.GetCommands(log, cfg)
	rootCmd.AddCommand(cmds...)
	clime.SuccessLine("Application started successfully!")

	if err := rootCmd.Execute(); err != nil {
		wrappedErr := fmt.Errorf("failed to execute root command: %w", err)
		log.Error("Command execution failed", "error", wrappedErr)
		clime.ErrorLine("Command execution failed: " + wrappedErr.Error())
		return wrappedErr
	}

	return nil
}
