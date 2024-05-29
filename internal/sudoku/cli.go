package sudoku

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"sudoku-golang/internal/sudoku/commands"
)

func Run() {

	var rootCmd = &cobra.Command{Use: "run"}
	cmds := commands.GetCommands()
	rootCmd.AddCommand(cmds...)
	err := rootCmd.Execute()
	if err != nil {
		logrus.Fatal(err)
	}

}
