package commands

import (
	"log/slog"
	"os"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/service"
)

func build(log *slog.Logger, config *configs.Config, debug *bool) {
	composer := service.NewComposer(log, false, os.Environ(), config, *debug)
	composer.Build()
}
