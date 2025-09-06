package sudoku

import (
	"log/slog"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/sudoku/commands"

	"github.com/alperdrsnn/clime"
	"github.com/spf13/cobra"
)

func Run(log *slog.Logger, cfg *configs.Config) error {
	clime.InfoLine("Starting application...")
	var rootCmd = &cobra.Command{Use: "run"}
	cmds := commands.GetCommands(log, cfg)
	rootCmd.AddCommand(cmds...)
	clime.SuccessLine("Application started successfully!")
	err := rootCmd.Execute()
	if err != nil {
		log.Error("Command execution failed", "error", err)
		clime.ErrorLine("Command execution failed: " + err.Error())
		return err
	}
	return nil
}
