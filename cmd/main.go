package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	var timeout int

	var rootCmd = &cobra.Command{
		Use:     "gitlab-sync",
		Version: version,
		Short:   "Copy and enable mirroring of gitlab projects and groups",
		Long:    "Fully customizable gitlab repositories and groups mirroring between two (or one) gitlab instances.",
		Run: func(cmd *cobra.Command, cmdArgs []string) {
			// Set up the logger
			setupZapLogger(args.Verbose)
			zap.L().Debug("Verbose mode enabled")
			zap.L().Debug("Parsing command line arguments")

			// Obtain the retry count
			if args.Retry == -1 {
				args.Retry = 10000
			} else if args.Retry == 0 {
				zap.L().Fatal("retry count must be -1 (no limit) or strictly greater than 0")
			}

			// Set the timeout for GitLab API requests
			if timeout == -1 {
				args.Timeout = time.Duration(10000 * time.Second)
			} else if timeout == 0 {
				zap.L().Fatal("timeout must be -1 (no limit) or strictly greater than 0")
			} else {
				args.Timeout = time.Duration(timeout) * time.Second
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
			mapping, err := utils.OpenMirrorMapping(mirrorMappingPath)
			if err != nil {
				zap.L().Sugar().Fatalf("Error opening mirror mapping file: %s", err)
			}
			zap.L().Debug("Mirror mapping file parsed successfully")
			args.MirrorMapping = mapping

			err = mirroring.MirrorGitlabs(&args)
			if err != nil {
				zap.L().Error("Error during mirroring process: " + err.Error())
			}
			zap.L().Info("Mirroring completed")
		},
	}

	rootCmd.Flags().StringVar(&args.SourceGitlabURL, "source-url", os.Getenv("SOURCE_GITLAB_URL"), "Source GitLab URL")
	rootCmd.Flags().StringVar(&args.SourceGitlabToken, "source-token", os.Getenv("SOURCE_GITLAB_TOKEN"), "Source GitLab Token")
	rootCmd.Flags().StringVar(&args.DestinationGitlabURL, "destination-url", os.Getenv("DESTINATION_GITLAB_URL"), "Destination GitLab URL")
	rootCmd.Flags().StringVar(&args.DestinationGitlabToken, "destination-token", os.Getenv("DESTINATION_GITLAB_TOKEN"), "Destination GitLab Token")
	rootCmd.Flags().BoolVarP(&args.Verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.Flags().BoolVarP(&args.NoPrompt, "no-prompt", "n", strings.TrimSpace(os.Getenv("NO_PROMPT")) != "", "Disable prompting for missing values")
	rootCmd.Flags().StringVar(&mirrorMappingPath, "mirror-mapping", os.Getenv("MIRROR_MAPPING"), "Path to the mirror mapping file")
	rootCmd.Flags().BoolVar(&args.DryRun, "dry-run", false, "Perform a dry run without making any changes")
	rootCmd.Flags().IntVarP(&timeout, "timeout", "t", 30, "Timeout in seconds for GitLab API requests")
	rootCmd.Flags().IntVarP(&args.Retry, "retry", "r", 3, "Number of retries for failed requests")

	if err := rootCmd.Execute(); err != nil {
		zap.L().Error(err.Error())
		os.Exit(1)
	}
}

func promptForInput(prompt string) string {
	var input string
	fmt.Printf("%s: ", prompt)
	fmt.Scanln(&input)
	return input
}

func promptForMandatoryInput(defaultValue string, prompt string, errorMsg string, loggerMsg string, promptsDisabled bool, hideOutput bool) string {
	input := strings.TrimSpace(defaultValue)
	if input == "" {
		if !promptsDisabled {
			input = strings.TrimSpace(promptForInput(prompt))
			if input == "" {
				zap.L().Fatal(errorMsg)
			}
			if !hideOutput {
				zap.L().Debug(loggerMsg + ": " + input)
			} else {
				zap.L().Debug(loggerMsg)
			}
		} else {
			zap.L().Sugar().Fatal("Prompting is disabled, %s", errorMsg)
		}
	}
	return input
}

func setupZapLogger(verbose bool) {
	// Set up the logger configuration
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	if verbose {
		config.Level.SetLevel(zapcore.DebugLevel)
	} else {
		config.Level.SetLevel(zapcore.InfoLevel)
	}

	// Create the logger
	logger, err := config.Build()
	if err != nil {
		zap.L().Fatal("Failed to create logger: " + err.Error())
	}

	// Set the global logger
	zap.ReplaceGlobals(logger)
}
