package commands

import (
	"os"
	"sudoku-golang/internal/service"
)

func build() {
	composer := service.NewComposer(false, os.Environ())
	composer.Build()
}
