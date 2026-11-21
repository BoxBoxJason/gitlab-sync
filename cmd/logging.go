package cmd

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
)

// logLevelFor returns the log level matching the verbosity requested in args.
func logLevelFor(args *mirrorcfg.ParserArgs) zapcore.Level {
	if args.Verbose {
		return zapcore.DebugLevel
	}

	return zapcore.InfoLevel
}

// SetupZapLogger sets up the Zap logger with the specified verbosity level and optional file output.
// It configures the logger to use ISO8601 time format and capitalizes the log levels.
// The logger is set to production mode by default, but can be configured for debug mode if verbose is true.
// If filename is not empty, the logger's output is redirected to the specified file.
func SetupZapLogger(level zapcore.Level, filename string) {
	// Set up the logger configuration
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	config.Level.SetLevel(level)

	// If a filename is specified, update the output path.
	if filename != "" {
		err := os.MkdirAll(filepath.Dir(filename), logDirPermission)
		if err != nil {
			zap.L().Fatal("Failed to create log directory: " + err.Error())
		}

		config.OutputPaths = []string{filename, "stderr"}
	}

	// Create the logger
	logger, err := config.Build()
	if err != nil {
		zap.L().Fatal("Failed to create logger: " + err.Error())
	}

	// Set the global logger
	zap.ReplaceGlobals(logger)
}
