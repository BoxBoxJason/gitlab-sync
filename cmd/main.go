package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitlab-sync/internal/mirroring"
	"gitlab-sync/internal/utils"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	version = "dev"
)

func main() {
	var args utils.ParserArgs
	var mirrorMappingPath string
	var logFile string

	var rootCmd = &cobra.Command{
		Use:     "gitlab-sync",
		Version: version,
		Short:   "Copy and enable mirroring of gitlab projects and groups",
		Long:    "Fully customizable gitlab repositories and groups mirroring between two (or one) gitlab instances.",
		Run: func(cmd *cobra.Command, cmdArgs []string) {
			// Set up the logger
			setupZapLogger(args.Verbose, strings.TrimSpace(logFile))
			zap.L().Debug("Verbose mode enabled")
			zap.L().Debug("Parsing command line arguments")

			// Obtain the retry count
			switch args.Retry {
			case -1:
				args.Retry = 10000
			case 0:
				zap.L().Fatal("retry count must be -1 (no limit) or strictly greater than 0")
			}

			// Check if the source GitLab URL is provided
			args.SourceGitlabURL = promptForMandatoryInput(args.SourceGitlabURL, "Input Source GitLab URL (MANDATORY)", "Source GitLab URL is mandatory", "Source GitLab URL", args.NoPrompt, false)

			// Check if the destination GitLab URL is provided
			args.DestinationGitlabURL = promptForMandatoryInput(args.DestinationGitlabURL, "Input Destination GitLab URL (MANDATORY)", "Destination GitLab URL is mandatory", "Destination GitLab URL", args.NoPrompt, false)

			// Check if the Destination GitLab Token is provided
			args.DestinationGitlabToken = promptForMandatoryInput(args.DestinationGitlabToken, "Input Destination GitLab Token with api permissions (MANDATORY)", "Destination GitLab Token is mandatory", "Destination GitLab Token set", args.NoPrompt, true)

			// Check if the Mirror Mapping file path is provided
			mirrorMappingPath = promptForMandatoryInput(mirrorMappingPath, "Input Mirror Mapping file path (MANDATORY)", "Mirror Mapping file path is mandatory", "Mirror Mapping file path set", args.NoPrompt, false)
			zap.L().Debug("Mirror Mapping file resolved path: " + filepath.Clean(mirrorMappingPath))

			zap.L().Debug("Parsing mirror mapping file")
			mapping, mappingErrors := utils.OpenMirrorMapping(mirrorMappingPath)
			if mappingErrors != nil {
				zap.L().Fatal("Error opening mirror mapping file", zap.Errors("errors", mappingErrors))
			}
			zap.L().Debug("Mirror mapping file parsed successfully")
			args.MirrorMapping = mapping

			mirroringErrors := mirroring.MirrorGitlabs(&args)
			if mirroringErrors != nil {
				zap.L().Error("Error during mirroring process", zap.Errors("errors", mirroringErrors))
			}
			zap.L().Info("Mirroring completed")
		},
	}

	rootCmd.Flags().StringVar(&args.SourceGitlabURL, "source-url", os.Getenv("SOURCE_GITLAB_URL"), "Source GitLab URL")
	rootCmd.Flags().StringVar(&args.SourceGitlabToken, "source-token", os.Getenv("SOURCE_GITLAB_TOKEN"), "Source GitLab Token")
	rootCmd.Flags().BoolVar(&args.SourceGitlabIsBig, "source-big", strings.TrimSpace(os.Getenv("SOURCE_GITLAB_BIG")) != "", "Source GitLab is a big instance")
	rootCmd.Flags().StringVar(&args.DestinationGitlabURL, "destination-url", os.Getenv("DESTINATION_GITLAB_URL"), "Destination GitLab URL")
	rootCmd.Flags().StringVar(&args.DestinationGitlabToken, "destination-token", os.Getenv("DESTINATION_GITLAB_TOKEN"), "Destination GitLab Token")
	rootCmd.Flags().BoolVar(&args.DestinationGitlabIsBig, "destination-big", strings.TrimSpace(os.Getenv("DESTINATION_GITLAB_BIG")) != "", "Destination GitLab is a big instance")
	rootCmd.Flags().BoolVarP(&args.ForcePremium, "destination-force-premium", "p", false, "Force the destination GitLab to be treated as a premium instance")
	rootCmd.Flags().BoolVarP(&args.ForceNonPremium, "destination-force-freemium", "f", false, "Force the destination GitLab to be treated as a non premium instance")
	rootCmd.Flags().BoolVarP(&args.Verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.Flags().BoolVarP(&args.NoPrompt, "no-prompt", "n", strings.TrimSpace(os.Getenv("NO_PROMPT")) != "", "Disable prompting for missing values")
	rootCmd.Flags().StringVar(&mirrorMappingPath, "mirror-mapping", os.Getenv("MIRROR_MAPPING"), "Path to the mirror mapping file")
	rootCmd.Flags().BoolVar(&args.DryRun, "dry-run", false, "Perform a dry run without making any changes")
	rootCmd.Flags().IntVarP(&args.Retry, "retry", "r", 3, "Number of retries for failed requests")
	rootCmd.Flags().StringVar(&logFile, "log-file", strings.TrimSpace(os.Getenv("GITLAB_SYNC_LOG_FILE")), "Path to the log file")

	if err := rootCmd.Execute(); err != nil {
		zap.L().Error(err.Error())
		os.Exit(1)
	}
}

// promptForInput prompts the user for input and returns the trimmed response.
// It handles errors and prints a message if the input is empty.
// If the input is empty, it will return an empty string.
func promptForInput(prompt string) string {
	var input string
	fmt.Printf("%s: ", prompt)
	_, err := fmt.Scanln(&input)
	if err != nil {
		zap.L().Fatal("Error reading input", zap.Error(err))
	}
	return strings.TrimSpace(input)
}

// promptForMandatoryInput prompts the user for mandatory input and returns the trimmed response.
// If the input is empty, it will log a fatal error message and exit the program.
// It also logs the input value if hideOutput is false.
func promptForMandatoryInput(defaultValue string, prompt string, errorMsg string, loggerMsg string, promptsDisabled bool, hideOutput bool) string {
	input := strings.TrimSpace(defaultValue)
	if input == "" {
		if !promptsDisabled {
			input = promptForInput(prompt)
			if input == "" {
				zap.L().Fatal(errorMsg)
			}
			if !hideOutput {
				zap.L().Debug(loggerMsg + ": " + input)
			} else {
				zap.L().Debug(loggerMsg)
			}
		} else {
			zap.L().Fatal("Prompting is disabled")
		}
	}
	return input
}

// setupZapLogger sets up the Zap logger with the specified verbosity level and optional file output.
// It configures the logger to use ISO8601 time format and capitalizes the log levels.
// The logger is set to production mode by default, but can be configured for debug mode if verbose is true.
// If filename is not empty, the logger's output is redirected to the specified file.
func setupZapLogger(verbose bool, filename string) {
	// Set up the logger configuration
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	if verbose {
		config.Level.SetLevel(zapcore.DebugLevel)
	} else {
		config.Level.SetLevel(zapcore.InfoLevel)
	}

	// If a filename is specified, update the output path.
	if filename != "" {
		err := os.MkdirAll(filepath.Dir(filename), 0700)
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
