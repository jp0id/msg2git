package logger

import (
	"io"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Logger *logrus.Logger

// InitLogger initializes the global logger with file rotation and appropriate levels
func InitLogger(logLevel string) error {
	Logger = logrus.New()

	// Create log directory if it doesn't exist
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	// Set up log rotation for different log levels
	errorLogPath := filepath.Join(logDir, "error.log")
	infoLogPath := filepath.Join(logDir, "info.log")
	debugLogPath := filepath.Join(logDir, "debug.log")

	// Configure lumberjack for log rotation
	errorLogger := &lumberjack.Logger{
		Filename:   errorLogPath,
		MaxSize:    10, // 10 MB
		MaxBackups: 5,
		MaxAge:     30, // 30 days
		Compress:   true,
	}

	infoLogger := &lumberjack.Logger{
		Filename:   infoLogPath,
		MaxSize:    10, // 10 MB
		MaxBackups: 5,
		MaxAge:     30, // 30 days
		Compress:   true,
	}

	debugLogger := &lumberjack.Logger{
		Filename:   debugLogPath,
		MaxSize:    10, // 10 MB
		MaxBackups: 5,
		MaxAge:     30, // 30 days
		Compress:   true,
	}

	// Set log level
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		level = logrus.InfoLevel // default to info if invalid level
	}
	Logger.SetLevel(level)

	// Set JSON formatter for structured logging
	Logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
	})

	// Configure hooks for different log levels to write to different files
	Logger.AddHook(&FileHook{
		ErrorWriter: errorLogger,
		InfoWriter:  infoLogger,
		DebugWriter: debugLogger,
	})

	// Also output to console for development
	Logger.SetOutput(os.Stdout)

	return nil
}

// FileHook implements logrus.Hook to write different log levels to different files
type FileHook struct {
	ErrorWriter io.Writer
	InfoWriter  io.Writer
	DebugWriter io.Writer
}

func (hook *FileHook) Fire(entry *logrus.Entry) error {
	line, err := entry.String()
	if err != nil {
		return err
	}

	switch entry.Level {
	case logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel:
		_, err = hook.ErrorWriter.Write([]byte(line))
	case logrus.WarnLevel, logrus.InfoLevel:
		_, err = hook.InfoWriter.Write([]byte(line))
	case logrus.DebugLevel, logrus.TraceLevel:
		_, err = hook.DebugWriter.Write([]byte(line))
	}

	return err
}

func (hook *FileHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Convenience functions for structured logging
func Error(msg string, fields map[string]interface{}) {
	if Logger != nil {
		Logger.WithFields(fields).Error(msg)
	}
}

func Info(msg string, fields map[string]interface{}) {
	if Logger != nil {
		Logger.WithFields(fields).Info(msg)
	}
}

func Debug(msg string, fields map[string]interface{}) {
	if Logger != nil {
		Logger.WithFields(fields).Debug(msg)
	}
}

func Warn(msg string, fields map[string]interface{}) {
	if Logger != nil {
		Logger.WithFields(fields).Warn(msg)
	}
}

// Simple logging functions without fields
func ErrorMsg(msg string) {
	Error(msg, nil)
}

func InfoMsg(msg string) {
	Info(msg, nil)
}

func DebugMsg(msg string) {
	Debug(msg, nil)
}

func WarnMsg(msg string) {
	Warn(msg, nil)
}