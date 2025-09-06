package logger

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

func NewLogger(logEnabled bool) *slog.Logger {
	service := os.Getenv("SERVICE_NAME")
	if service == "" {
		service = "sudoku-golang"
	}
	stage := os.Getenv("STAGE")
	if stage == "" {
		stage = "local"
	}
	level := os.Getenv("LOG_LEVEL")
	if level == "" {
		level = "info"
	}

	var l slog.Level
	if !logEnabled {
		l = slog.LevelError
	} else {
		switch strings.ToLower(level) {
		case "debug":
			l = slog.LevelDebug
		case "info":
			l = slog.LevelInfo
		case "warn":
			l = slog.LevelWarn
		case "error":
			l = slog.LevelError
		default:
			l = slog.LevelInfo
		}
	}

	logOpts := slog.HandlerOptions{
		Level:       l,
		AddSource:   true,
		ReplaceAttr: replaceAttr,
	}
	logHandler := slog.NewJSONHandler(os.Stdout, &logOpts)

	return slog.New(logHandler).
		With(slog.String("stage", stage)).
		With(slog.String("service", service))
}

func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case "time":
		return slog.String("timestamp", a.Value.Time().Format(time.RFC3339))
	case "msg":
		return slog.String("rest", a.Value.String())
	case "level":
		return slog.String("severity", a.Value.String())
	default:
		return a
	}
}
