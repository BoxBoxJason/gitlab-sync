package cmd

import (
	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	"github.com/spf13/cobra"
)

// addConcurrencyFlags registers the flags bounding the work in flight and
// returns the problems found in their environment variables.
func addConcurrencyFlags(rootCmd *cobra.Command, args *mirrorcfg.ParserArgs) []error {
	var errs []error

	apiConcurrency, err := helpers.EnvInt("GITLAB_SYNC_API_CONCURRENCY", defaultAPIConcurrency)
	if err != nil {
		errs = append(errs, err)
	}

	gitConcurrency, err := helpers.EnvInt("GITLAB_SYNC_GIT_CONCURRENCY", defaultGitConcurrency)
	if err != nil {
		errs = append(errs, err)
	}

	rootCmd.Flags().IntVar(&args.APIConcurrency, "api-concurrency", apiConcurrency, "Maximum number of GitLab API requests in flight at once, per instance (0 or less: unbounded)")
	rootCmd.Flags().IntVar(&args.GitConcurrency, "git-concurrency", gitConcurrency, "Maximum number of git clones and pushes running at once (freemium mirroring only, 0 or less: unbounded)")

	return errs
}
