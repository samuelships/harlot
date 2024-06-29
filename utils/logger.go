package utils

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/natefinch/lumberjack"
)

var Logger *slog.Logger

func NewProdLogger() *slog.Logger {
	lw := &lumberjack.Logger{
		Filename:   "./logs/log.log",
		MaxSize:    5,
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	mw := io.MultiWriter(lw, os.Stdout)
	logger := slog.New(slog.NewTextHandler(mw, nil))
	return logger
}

func NewTestLogger() *slog.Logger {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return logger
}

func InitLogger() {
	Logger = NewProdLogger()
}

func LogErrorReturn(message string, args ...any) error {
	Logger.Error(message, args...)
	return fmt.Errorf(message, args...)
}

func LogInfo(msg string, args ...any) {
	Logger.Info(msg, args...)
}

func LogError(msg string, args ...any) {
	Logger.Error(msg, args...)
}

func LogDebug(msg string, args ...any) {
	Logger.Debug(msg, args...)
}

func LogWarn(msg string, args ...any) {
	Logger.Warn(msg, args...)
}
