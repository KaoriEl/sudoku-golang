package commands

import (
	"os"
	"sudoku-golang/internal/service"
)

func restart() {
	composer := service.NewComposer(false, os.Environ())

	composer.Stop()
	composer.Start()
}
