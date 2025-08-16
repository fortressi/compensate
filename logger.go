package compensate

import (
	"log/slog"
)

// Logger interface allows users to provide custom loggers
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	With(args ...any) Logger
	WithGroup(name string) Logger
}

// slogAdapter adapts slog.Logger to our Logger interface
type slogAdapter struct {
	logger *slog.Logger
}

func (s *slogAdapter) Debug(msg string, args ...any) { s.logger.Debug(msg, args...) }
func (s *slogAdapter) Info(msg string, args ...any)  { s.logger.Info(msg, args...) }
func (s *slogAdapter) Warn(msg string, args ...any)  { s.logger.Warn(msg, args...) }
func (s *slogAdapter) Error(msg string, args ...any) { s.logger.Error(msg, args...) }

func (s *slogAdapter) With(args ...any) Logger {
	return &slogAdapter{logger: s.logger.With(args...)}
}

func (s *slogAdapter) WithGroup(name string) Logger {
	return &slogAdapter{logger: s.logger.WithGroup(name)}
}

// DefaultLogger returns a logger using slog.Default()
func DefaultLogger() Logger {
	return &slogAdapter{logger: slog.Default()}
}

// NopLogger returns a no-op logger for backward compatibility
func NopLogger() Logger {
	return &nopLogger{}
}

type nopLogger struct{}

func (n *nopLogger) Debug(msg string, args ...any) {}
func (n *nopLogger) Info(msg string, args ...any)  {}
func (n *nopLogger) Warn(msg string, args ...any)  {}
func (n *nopLogger) Error(msg string, args ...any) {}
func (n *nopLogger) With(args ...any) Logger       { return n }
func (n *nopLogger) WithGroup(name string) Logger  { return n }