package main

import (
	"errors"
	"fmt"
	"os"
	"sudoku-golang/internal/doctor"
	"sudoku-golang/internal/selfupdate"
	"sudoku-golang/internal/service"
	"sudoku-golang/internal/sudoku"

	"github.com/alperdrsnn/clime"
)

func main() {
	selfupdate.CleanupOld()
	if err := run(); err != nil {
		if !errors.Is(err, service.ErrFailed) && !errors.Is(err, doctor.ErrFailed) {
			clime.ErrorLine(err.Error())
		}
		os.Exit(1)
	}
}

func run() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return sudoku.Run() //nolint:wrapcheck // errors are already user-facing
}
