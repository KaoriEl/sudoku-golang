package commands

import (
	"os"
	"sudoku-golang/internal/service"
)

func rebuild() {
	composer := service.NewComposer(false, os.Environ())

	composer.Stop()
	composer.Build()
	composer.Start()
}
