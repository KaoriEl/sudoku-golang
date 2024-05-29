package commands

import (
	"os"
	"sudoku-golang/internal/service"
)

func forceRebuild() {
	composer := service.NewComposer(true, os.Environ())

	composer.Stop()
	composer.Build()
	composer.Start()
}
