package commands

import (
	"os"
	"sudoku-golang/internal/service"
)

func start() {
	composer := service.NewComposer(false, os.Environ())

	composer.Start()
}
