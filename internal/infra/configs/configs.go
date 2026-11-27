package configs

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DockerPath        string
	DockerComposePath string
	MaxWorkers        int
	// RootProjectsFolder is absolute: ~ expanded, relative values resolved against Home.
	RootProjectsFolder string
	// Home holds .env; DataDir holds the infra compose.yaml, sudoku-config/ and backups (see DataDirFor).
	Home      string
	DataDir   string
	AdminAddr string
	// AdminToken, when set, is required by the admin (cookie after ?token=…, or Authorization: Bearer).
	AdminToken string
}

// DefaultAdminAddr is where `sudoku serve` listens when neither --addr nor ADMIN_ADDR is set.
const DefaultAdminAddr = "127.0.0.1:7777"

// EnvFileName is the dotenv file read from the sudoku home (see ResolveHome).
const EnvFileName = ".env"

// dotenvKeys are the variables that were set by the .env file (not by the real environment).
var dotenvKeys = map[string]bool{}

var secretsPath = "/vault/secrets/"

func MustLoad() (*Config, error) {
	before := map[string]bool{}
	for _, kv := range os.Environ() {
		if k, _, ok := strings.Cut(kv, "="); ok {
			before[k] = true
		}
	}
	home = ResolveHome()
	if err := godotenv.Load(filepath.Join(home, EnvFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("Failed to load .env file", slog.Any("error", err))
	}
	for _, kv := range os.Environ() {
		if k, _, ok := strings.Cut(kv, "="); ok && !before[k] {
			dotenvKeys[k] = true
		}
	}

	cfg := &Config{
		DockerPath:         readValueFromFileOrEnv("DOCKER_PATH", DefaultDockerPath),
		DockerComposePath:  readValueFromFileOrEnv("DOCKER_COMPOSE_PATH", DefaultDockerComposePath()),
		MaxWorkers:         clampWorkers(readValueAsInt("MAX_WORKERS", defaultWorkers)),
		RootProjectsFolder: ExpandPath(readOptional("ROOT_PROJECTS_FOLDER", ""), home),
		Home:               home,
		AdminAddr:          readOptional("ADMIN_ADDR", DefaultAdminAddr),
		AdminToken:         readOptional("ADMIN_TOKEN", ""),
	}

	cfg.DataDir = DataDirFor(home, cfg.RootProjectsFolder)

	return cfg, nil
}

const (
	defaultWorkers = 5
	maxWorkers     = 64
)

// clampWorkers keeps MAX_WORKERS in 1..64: 0 or a negative value would mean "no parallelism at all"
// for the admin's semaphore, and a huge value only spawns hundreds of docker processes at once.
func clampWorkers(n int) int {
	return max(1, min(maxWorkers, n))
}

func readValueFromFileOrEnv(valueName string, defaultValue string) string {
	// Secret files usually end with a newline; an untrimmed "/path\n" breaks paths and commands.
	value, err := os.ReadFile(secretsPath + "CRED_" + valueName)
	if err == nil {
		return strings.TrimSpace(string(value))
	}
	if v := os.Getenv(valueName); v != "" {
		return v
	}
	slog.Debug("Config value not found, using default", slog.String("key", valueName), slog.String("default", defaultValue))
	return defaultValue
}

// readOptional is readValueFromFileOrEnv without the "using default" warning, for keys most
// .env files do not have.
func readOptional(valueName string, defaultValue string) string {
	if value, err := os.ReadFile(secretsPath + "CRED_" + valueName); err == nil {
		return strings.TrimSpace(string(value))
	}
	if v := os.Getenv(valueName); v != "" {
		return v
	}
	return defaultValue
}

func readValueAsInt(valueName string, defaultValue int) int {
	str := readValueFromFileOrEnv(valueName, "")
	if str == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(strings.TrimSpace(str))
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

// DefaultDockerPath is resolved through PATH on every OS (docker.exe on Windows).
const DefaultDockerPath = "docker"

// DefaultDockerComposePath prefers a standalone docker-compose when one is installed (what older
// setups used), otherwise the compose plugin (`docker compose`), which every current Docker Desktop,
// Colima, OrbStack and Linux package ships. LookPath is a stat per PATH entry, no process spawn.
func DefaultDockerComposePath() string {
	if _, err := exec.LookPath("docker-compose"); err == nil {
		return "docker-compose"
	}
	return "docker compose"
}

// Source tells where a config value currently comes from: "secret" (/vault/secrets file),
// "env" (real process environment, overrides .env), "file" (.env) or "default".
func Source(key string) string {
	if _, err := os.Stat(secretsPath + "CRED_" + key); err == nil {
		return "secret"
	}
	if _, ok := os.LookupEnv(key); ok {
		if dotenvKeys[key] {
			return "file"
		}
		return "env"
	}
	return "default"
}

// CleanEnviron is os.Environ() without the variables that came from .env, so a re-executed
// process reads the (possibly edited) .env file again instead of inheriting stale values.
func CleanEnviron() []string {
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if k, _, ok := strings.Cut(kv, "="); ok && dotenvKeys[k] {
			continue
		}
		out = append(out, kv)
	}
	return out
}
