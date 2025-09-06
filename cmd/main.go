package main

import (
	"fmt"
	"os"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/logger"
	"sudoku-golang/internal/sudoku"

	"github.com/alperdrsnn/clime"
)

func main() {
	clime.Header("🧩  Sudoku Golang CLI 🧩")

	log := logger.NewLogger(false)
	log.Info("Logger initialized", "logEnabled", false)

	cfg, err := configs.MustLoad()
	if err != nil {
		clime.ErrorLine("Failed to load config: " + err.Error())
		os.Exit(1)
	}

	clime.InfoLine("Config loaded successfully")

	if err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				clime.ErrorLine("Application panicked: " + fmt.Sprint(r))
				err = fmt.Errorf("panic occurred: %v", r)
			}
		}()

		return sudoku.Run(log, cfg)
	}(); err != nil {
		clime.ErrorLine("Application finished with error: " + err.Error())
		os.Exit(1)
	}

	clime.SuccessLine("Application finished successfully!")
}
