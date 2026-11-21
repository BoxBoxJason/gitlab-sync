package mirroring

import (
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

// DryRun prints the groups and projects that would be created or updated in dry run mode.
func (g *GitlabInstance) DryRun(sourceGitlabInstance *GitlabInstance, mirrorMapping *mirrorcfg.MirrorMapping) {
	zap.L().Info("Dry run mode enabled, will not create groups or projects")
	zap.L().Info("Groups that will be created (or updated if they already exist):")

	for sourceGroupPath, copyOptions := range mirrorMapping.GroupsSnapshot() {
		if sourceGroup := sourceGitlabInstance.GetGroup(sourceGroupPath); sourceGroup != nil {
			_, err := fmt.Fprintf(os.Stdout, "  - %s (source gitlab) -> %s (destination gitlab)\n", sourceGroup.WebURL, copyOptions.DestinationPath)
			if err != nil {
				helpers.ReportNonBlocking(fmt.Errorf("failed to print group dry-run output: %w", err))

				return
			}
		}
	}

	zap.L().Info("Projects that will be created (or updated if they already exist):")

	for sourceProjectPath, copyOptions := range mirrorMapping.ProjectsSnapshot() {
		if sourceProject := sourceGitlabInstance.GetProject(sourceProjectPath); sourceProject != nil {
			_, err := fmt.Fprintf(os.Stdout, "  - %s (source gitlab) -> %s (destination gitlab)\n", sourceProject.WebURL, copyOptions.DestinationPath)
			if err != nil {
				helpers.ReportNonBlocking(fmt.Errorf("failed to print project dry-run output: %w", err))

				return
			}

			if helpers.Deref(copyOptions.MirrorReleases, false) {
				err = g.DryRunReleases(sourceGitlabInstance, sourceProject, copyOptions)
				if err != nil {
					helpers.ReportNonBlocking(fmt.Errorf("failed to dry run releases: %w", err))

					return
				}
			}
		}
	}

	zap.L().Info("Dry run completed")
}

// IsPullMirrorAvailable checks the destination GitLab instance for version and license compatibility.
func (g *GitlabInstance) IsPullMirrorAvailable(args *mirrorcfg.ParserArgs) (bool, error) {
	zap.L().Info("Checking destination GitLab instance")

	thresholdOk, err := g.IsVersionGreaterThanThreshold()
	if err != nil {
		return false, fmt.Errorf("destination GitLab instance version check failed: %w", err)
	}

	isPremium, err := g.IsLicensePremium()
	if err != nil {
		if !args.ForcePremium && !args.ForceNonPremium {
			return false, fmt.Errorf("failed to check if destination GitLab instance is premium: %w", err)
		}
	}

	return !args.ForceNonPremium && (thresholdOk && (isPremium || args.ForcePremium)), nil
}
