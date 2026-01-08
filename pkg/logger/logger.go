// Package logger provides structured logging for NeuroGate services
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// Logger wraps slog.Logger with service-specific context
type Logger struct {
	*slog.Logger
}

// Config holds logger configuration
type Config struct {
	Level   string // debug, info, warn, error
	Service string // Service name for tagging logs
	JSON    bool   // Whether to output JSON format
}

// New creates a new structured logger
func New(cfg Config) *Logger {
	level := parseLevel(cfg.Level)

	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level == slog.LevelDebug,
	}

	if cfg.JSON {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	logger := slog.New(handler).With(
		slog.String("service", cfg.Service),
	)

	return &Logger{Logger: logger}
}

// Default creates a logger with default settings
func Default(service string) *Logger {
	return New(Config{
		Level:   "info",
		Service: service,
		JSON:    false,
	})
}

// WithRequestID returns a logger with request ID context
func (l *Logger) WithRequestID(requestID string) *Logger {
	return &Logger{
		Logger: l.Logger.With(slog.String("request_id", requestID)),
	}
}

// WithWorker returns a logger with worker context
func (l *Logger) WithWorker(workerID string, addr string) *Logger {
	return &Logger{
		Logger: l.Logger.With(
			slog.String("worker_id", workerID),
			slog.String("worker_addr", addr),
		),
	}
}

// WithError returns a logger with error context
func (l *Logger) WithError(err error) *Logger {
	return &Logger{
		Logger: l.Logger.With(slog.String("error", err.Error())),
	}
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
