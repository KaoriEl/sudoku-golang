package commands

import (
	"log/slog"
	"os"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/service"
)

func start(log *slog.Logger, config *configs.Config) {
	composer := service.NewComposer(log, false, os.Environ(), config)

	composer.Start()
}
