package commands

import (
	"os"
	"sudoku-golang/internal/service"
)

func stop() {
	composer := service.NewComposer(false, os.Environ())
	composer.Stop()
}
