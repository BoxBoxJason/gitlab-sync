package mirroring

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
)

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
