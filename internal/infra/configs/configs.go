package configs

import (
	"log/slog"
	"os"
	"runtime"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DockerPath         string
	DockerComposePath  string
	MaxWorkers         int
	RootProjectsFolder string
}

var secretsPath = "/vault/secrets/"

func MustLoad() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		slog.Warn("Failed to load .env file", slog.Any("error", err))
	}

	cfg := &Config{
		DockerPath:         readValueFromFileOrEnv("DOCKER_PATH", "/usr/bin/env docker"),
		DockerComposePath:  readValueFromFileOrEnv("DOCKER_COMPOSE_PATH", defaultDockerComposePath()),
		MaxWorkers:         readValueAsInt("MAX_WORKERS", 5),
		RootProjectsFolder: readValueFromFileOrEnv("ROOT_PROJECTS_FOLDER", "projects"),
	}

	return cfg, nil
}

func readValueFromFileOrEnv(valueName string, defaultValue string) string {
	value, err := os.ReadFile(secretsPath + "CRED_" + valueName)
	if err == nil {
		return string(value)
	}
	if v := os.Getenv(valueName); v != "" {
		return v
	}
	slog.Warn("Config value not found, using default", slog.String("key", valueName), slog.String("default", defaultValue))
	return defaultValue
}

func readValueAsInt(valueName string, defaultValue int) int {
	str := readValueFromFileOrEnv(valueName, "")
	if str == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(str)
	if err != nil {
		slog.Warn(
			"Failed to parse int config value, using default",
			slog.String("key", valueName),
			slog.String("value", str),
			slog.Int("default", defaultValue),
			slog.Any("error", err),
		)
		return defaultValue
	}
	return n
}

func defaultDockerComposePath() string {
	if runtime.GOOS == "darwin" {
		return "docker-compose"
	}
	return "/usr/bin/env docker-compose"
}
