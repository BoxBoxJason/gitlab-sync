package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/boxboxjason/gitlab-sync/internal/mirroring"
	"github.com/boxboxjason/gitlab-sync/internal/utils"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	defaultRetryCount   = 3
	logDirPermission    = 0o700
	nonBlockingExitCode = 2
)

// version and buildTime can optionally be set at build time via -ldflags.
// When they are unset (for example with `go install`), version resolution
// falls back to runtime/debug.ReadBuildInfo.
//
//nolint:gochecknoglobals // intentional ldflags injection targets
var (
	version   string // set via -X github.com/boxboxjason/gitlab-sync/cmd.version=vX.Y.Z
	buildTime string // set via -X github.com/boxboxjason/gitlab-sync/cmd.buildTime=2006-01-02T15:04:05Z
)

// Execute runs the CLI command.
func Execute() {
	var (
		args              utils.ParserArgs
		err               error
		mirrorMappingPath string
		logFile           string
	)

	rootCmd := buildRootCmd(&args, &mirrorMappingPath, &logFile)

	err = rootCmd.Execute()
	if err != nil {
		zap.L().Error(err.Error())
		os.Exit(1)
	}
}

func buildRootCmd(args *utils.ParserArgs, mirrorMappingPath, logFile *string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "gitlab-sync",
		Version: versionInfo(),
		Short:   "Copy and enable mirroring of gitlab projects and groups",
		Long:    "Fully customizable gitlab repositories and groups mirroring between two (or one) gitlab instances.",
		Run: func(cmd *cobra.Command, cmdArgs []string) {
			executeMirroringCommand(args, mirrorMappingPath, logFile)
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
	rootCmd.Flags().StringVar(mirrorMappingPath, "mirror-mapping", os.Getenv("MIRROR_MAPPING"), "Path to the mirror mapping file")
	rootCmd.Flags().BoolVar(&args.DryRun, "dry-run", false, "Perform a dry run without making any changes")
	rootCmd.Flags().IntVarP(&args.Retry, "retry", "r", defaultRetryCount, "Number of retries for failed requests")
	rootCmd.Flags().StringVar(logFile, "log-file", strings.TrimSpace(os.Getenv("GITLAB_SYNC_LOG_FILE")), "Path to the log file")
	_ = rootCmd.MarkFlagFilename("mirror-mapping", "json")
	_ = rootCmd.MarkFlagFilename("log-file", "log", "txt")

	addCompletionCommand(rootCmd)

	return rootCmd
}

func addCompletionCommand(rootCmd *cobra.Command) {
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	completionCmd := &cobra.Command{
		Use:                   "completion [bash|zsh|fish|powershell]",
		Short:                 "Generate shell completion script",
		Long:                  "Generate shell completion script for gitlab-sync.",
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			switch cmdArgs[0] {
			case "bash":
				return rootCmd.GenBashCompletionV2(cmd.OutOrStdout(), true)
			case "zsh":
				return rootCmd.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell: %s", cmdArgs[0])
			}
		},
	}

	rootCmd.AddCommand(completionCmd)
}

func executeMirroringCommand(args *utils.ParserArgs, mirrorMappingPath, logFile *string) {
	// Set up the logger
	SetupZapLogger(args.Verbose, strings.TrimSpace(*logFile))
	zap.L().Debug("Verbose mode enabled")
	zap.L().Debug("Parsing command line arguments")

	// Obtain the retry count
	switch args.Retry {
	case -1:
		args.Retry = 10000
	case 0:
		zap.L().Fatal("retry count must be -1 (no limit) or strictly greater than 0")
	}

	args.SourceGitlabURL = promptForMandatoryInput(args.SourceGitlabURL, "Input Source GitLab URL (MANDATORY)", "Source GitLab URL is mandatory", "Source GitLab URL", args.NoPrompt, false)
	args.DestinationGitlabURL = promptForMandatoryInput(args.DestinationGitlabURL, "Input Destination GitLab URL (MANDATORY)", "Destination GitLab URL is mandatory", "Destination GitLab URL", args.NoPrompt, false)
	args.DestinationGitlabToken = promptForMandatoryInput(args.DestinationGitlabToken, "Input Destination GitLab Token with api permissions (MANDATORY)", "Destination GitLab Token is mandatory", "Destination GitLab Token set", args.NoPrompt, true)

	*mirrorMappingPath = promptForMandatoryInput(*mirrorMappingPath, "Input Mirror Mapping file path (MANDATORY)", "Mirror Mapping file path is mandatory", "Mirror Mapping file path set", args.NoPrompt, false)
	zap.L().Debug("Mirror Mapping file resolved path: " + filepath.Clean(*mirrorMappingPath))

	zap.L().Debug("Parsing mirror mapping file")

	mapping, mappingErrors := utils.OpenMirrorMapping(*mirrorMappingPath)
	if mappingErrors != nil {
		zap.L().Fatal("Error opening mirror mapping file", zap.Errors("errors", mappingErrors))
	}

	args.MirrorMapping = mapping

	mirroringErrors := mirroring.MirrorGitlabs(args)
	if mirroringErrors == nil {
		zap.L().Info("Mirroring completed successfully")

		return
	}

	hasBlocking := false

	for _, currentErr := range mirroringErrors {
		if helpers.SeverityOf(currentErr) == helpers.SeverityBlocking {
			hasBlocking = true

			break
		}
	}

	if hasBlocking {
		zap.L().Error("Blocking errors occurred during mirroring process", zap.Errors("errors", mirroringErrors))
		os.Exit(1)
	}

	zap.L().Warn("Non-blocking errors occurred during mirroring process", zap.Errors("errors", mirroringErrors))
	os.Exit(nonBlockingExitCode)
}

// promptForInput prompts the user for input and returns the trimmed response.
// It handles errors and prints a message if the input is empty.
// If the input is empty, it will return an empty string.
func promptForInput(prompt string) string {
	var input string

	_, err := fmt.Fprintf(os.Stdout, "%s: ", prompt)
	if err != nil {
		zap.L().Fatal("Error writing prompt", zap.Error(err))
	}

	_, err = fmt.Scanln(&input)
	if err != nil {
		zap.L().Fatal("Error reading input", zap.Error(err))
	}

	return strings.TrimSpace(input)
}

// promptForMandatoryInput prompts the user for mandatory input and returns the trimmed response.
// If the input is empty, it will log a fatal error message and exit the program.
// It also logs the input value if hideOutput is false.
func promptForMandatoryInput(defaultValue, prompt, errorMsg, loggerMsg string, promptsDisabled, hideOutput bool) string {
	input := strings.TrimSpace(defaultValue)
	if input != "" {
		return input
	}

	if promptsDisabled {
		zap.L().Fatal("Prompting is disabled")
	}

	input = promptForInput(prompt)
	if input == "" {
		zap.L().Fatal(errorMsg)
	}

	if !hideOutput {
		zap.L().Debug(loggerMsg + ": " + input)
	} else {
		zap.L().Debug(loggerMsg)
	}

	return input
}

// SetupZapLogger sets up the Zap logger with the specified verbosity level and optional file output.
// It configures the logger to use ISO8601 time format and capitalizes the log levels.
// The logger is set to production mode by default, but can be configured for debug mode if verbose is true.
// If filename is not empty, the logger's output is redirected to the specified file.
func SetupZapLogger(verbose bool, filename string) {
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

// versionInfo returns a human-readable version string of the form:
//
//	v1.2.3 (go1.25.7, built: 2026-02-22T20:00:00Z)
//
// Version resolution order:
//  1. ldflags-injected version  (make build / make build version=x.y.z)
//  2. Module version from debug.ReadBuildInfo  (go install @vX.Y.Z)
//  3. VCS commit hash from build settings  (local go build / go install @latest)
//  4. "dev" as final fallback
//
// Build time resolution order:
//  1. ldflags-injected buildTime  (make build)
//  2. vcs.time from build settings
//  3. "unknown"
func versionInfo() string {
	ver := resolveVersion()
	bt := resolveBuildTime()

	return fmt.Sprintf("%s (go: %s, built: %s)", ver, runtime.Version(), bt)
}

// resolveVersion returns the most specific version string available.
func resolveVersion() string {
	// ldflags injection wins — used by `make build` and CI releases.
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	// `go install @vX.Y.Z` populates Main.Version with the module tag.
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	// Local `go build` / `go install @latest` — fall back to VCS revision.
	return vcsRevision(info)
}

// vcsRevision extracts the short commit hash (and a "-dirty" suffix when the
// working tree has uncommitted changes) from the VCS build settings.
func vcsRevision(info *debug.BuildInfo) string {
	var revision string

	var dirty bool

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if len(setting.Value) > 7 { //nolint:mnd // 7 is the conventional short-hash length
				revision = setting.Value[:7]
			} else {
				revision = setting.Value
			}
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}

	if revision == "" {
		return "dev"
	}

	if dirty {
		return revision + "-dirty"
	}

	return revision
}

// resolveBuildTime returns the build timestamp, falling back through ldflags →
// vcs.time build setting → "unknown".
func resolveBuildTime() string {
	if buildTime != "" {
		return buildTime
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	for _, setting := range info.Settings {
		if setting.Key == "vcs.time" {
			return setting.Value
		}
	}

	return "unknown"
}
