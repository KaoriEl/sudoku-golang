package commands

import (
	"os"
	"sudoku-golang/internal/service"
)

func down() {
	composer := service.NewComposer(false, os.Environ())

	composer.Down()
}
