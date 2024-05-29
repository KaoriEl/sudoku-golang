package configs

import (
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"github.com/vrischmann/envconfig"
	"os"
)

type Configs struct {
	ConfigDockerPaths ConfigDockerPaths
	ConfigWorker      ConfigWorker
	ConfigProject     ConfigProject
}

// InitConfigs Инициализруем конфиги
func InitConfigs() (*Configs, error) {
	envData, err := godotenv.Read("./sudoku-config/.env")

	if err != nil {
		logrus.Fatal("init env failed: %w", err)
	}

	for envKey, envValue := range envData {
		err := os.Setenv(envKey, envValue)
		if err != nil {
			return nil, err
		}
	}

	configs := Configs{}
	if err := envconfig.Init(&configs); err != nil {
		return Configs{}, err
	}

	return configs, nil
}
