package sudoku

import (
	"sudoku-golang/internal/sudoku/commands"
)

// Run builds the command tree and executes it.
func Run() error {
	return commands.Root().Execute() //nolint:wrapcheck // cobra already printed the error
}
