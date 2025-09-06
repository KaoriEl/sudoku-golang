package commands

import (
	"log/slog"
	"os"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/service"
)

func forceRebuild(log *slog.Logger, config *configs.Config) {
	composer := service.NewComposer(log, true, os.Environ(), config)

	composer.Stop()
	composer.Build()
	composer.Start()
}
