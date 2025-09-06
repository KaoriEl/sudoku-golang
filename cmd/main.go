package main

import (
	"os"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/logger"
	"sudoku-golang/internal/sudoku"

	"github.com/alperdrsnn/clime"
)

func main() {
	clime.Header("🧩 SUDOKU 🧩")

	log := logger.NewLogger(false)
	log.Info("Logger initialized", "logEnabled")

	defer func() {
		if r := recover(); r != nil {
			clime.ErrorLine("Application panicked: " + r.(string))

			os.Exit(1)
		}
	}()

	cfg, err := configs.MustLoad()
	if err != nil {
		clime.ErrorLine("Failed to load config: " + err.Error())

		os.Exit(1)
	}

	clime.InfoLine("Config loaded successfully")

	if err := sudoku.Run(log, cfg); err != nil {
		clime.ErrorLine("Application finished with error: " + err.Error())

		os.Exit(1)
	}

	clime.SuccessLine("Application finished successfully!")
}
