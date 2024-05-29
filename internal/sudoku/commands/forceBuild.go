package commands

import (
	"os"
	"sudoku-golang/internal/service"
)

func forceBuild() {
	composer := service.NewComposer(true, os.Environ())

	composer.Build()
}
