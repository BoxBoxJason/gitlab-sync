package mirroring

import (
	"errors"
	"fmt"
	"time"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"
)

const (
	projectsPerPage         = 100
	updateProjectBaseTasks  = 2
	projectOwnerAccessLevel = 50
)

// ===========================================================================
//                          PROJECTS GET FUNCTIONS                          //
// ===========================================================================

// ===========================================================================
//                         SMALL INSTANCE FUNCTIONS                         //
// ===========================================================================

// ===========================================================================
//                         LARGE INSTANCE FUNCTIONS                         //
// ===========================================================================

// ============================================================ //
//                 PROJECT CREATION FUNCTIONS                   //
// ============================================================ //

// ===========================================================================
//                          PROJECTS PUT FUNCTIONS                          //
// ===========================================================================

// MirrorProjectGit mirrors the git content of the source project, by pull mirroring when available or by pushing otherwise.
func (g *GitlabInstance) MirrorProjectGit(sourceGitlabInstance *GitlabInstance, sourceProject, destinationProject *gitlab.Project, mirrorOptions *mirrorcfg.MirroringOptions) error {
	if g.PullMirrorAvailable {
		return g.EnableProjectMirrorPull(sourceProject, destinationProject, mirrorOptions)
	}

	err := g.DisableProjectMirrorPull(destinationProject)
	if err != nil {
		return err
	}

	err = helpers.MirrorRepo(g.GitCache, sourceProject.HTTPURLToRepo, destinationProject.HTTPURLToRepo, sourceGitlabInstance.GitAuth, g.GitAuth, mirrorPushOptions(mirrorOptions))
	if err != nil {
		return fmt.Errorf("failed to mirror repository from %s to %s: %w", sourceProject.PathWithNamespace, destinationProject.PathWithNamespace, err)
	}

	return nil
}

// mirrorPushOptions returns how the git push of a freemium mirroring run is made.
//
// Like a pull mirror with mirror_trigger_builds off, the push must not run CI on
// the destination for the branches and tags it updates, unless the option is
// explicitly enabled. Missing options count as disabled.
func mirrorPushOptions(mirrorOptions *mirrorcfg.MirroringOptions) helpers.MirrorPushOptions {
	triggerBuilds := mirrorOptions != nil && mirrorOptions.TriggerBuilds()

	return helpers.MirrorPushOptions{SkipPipelines: !triggerBuilds}
}

// DisableProjectMirrorPull turns off pull mirroring on a destination project.
//
// GitLab keeps a pull-mirrored repository read-only, so a project left over from an
// earlier premium run (or a premium/ultimate destination now driven with
// --destination-force-freemium) would reject the clone/push mirroring that follows.
func (g *GitlabInstance) DisableProjectMirrorPull(destinationProject *gitlab.Project) error {
	if !destinationProject.Mirror {
		return nil
	}

	zap.L().Info("Disabling pull mirror on destination project before pushing", zap.String("project", destinationProject.PathWithNamespace))

	updatedProject, _, err := g.Gitlab.Projects.EditProject(destinationProject.ID, &gitlab.EditProjectOptions{
		Mirror: new(false),
	})
	if err != nil {
		return fmt.Errorf("failed to disable pull mirror for project %s: %w", destinationProject.PathWithNamespace, err)
	}

	destinationProject.Mirror = false

	if updatedProject != nil {
		g.AddProject(updatedProject)
	}

	return nil
}

// EnableProjectMirrorPull (re)configures the pull mirror for a project in the destination GitLab instance.
// It sets the source project URL, enables mirroring, and configures other options like triggering builds and overwriting diverged branches.
//
// This is called on every run, for every project (not just newly created ones): GitLab's pull
// mirror update endpoint unconditionally reassigns the mirror's "running user" to whichever user
// makes this call, so calling it every time is what allows changing the user running the script.
// That reassignment only takes effect if the calling user has Maintainer+ access on the
// destination project, which is why ClaimOwnershipToProject also needs to run on every run.
func (g *GitlabInstance) EnableProjectMirrorPull(sourceProject, destinationProject *gitlab.Project, mirrorOptions *mirrorcfg.MirroringOptions) error {
	zap.L().Debug("Reapplying project mirror pull", zap.String("sourceProject", sourceProject.HTTPURLToRepo), zap.String("destinationProject", destinationProject.HTTPURLToRepo))

	desiredMirrorTriggerBuilds := mirrorOptions.TriggerBuilds()

	_, _, err := g.Gitlab.Projects.ConfigureProjectPullMirror(destinationProject.ID, &gitlab.ConfigureProjectPullMirrorOptions{
		URL:                              &sourceProject.HTTPURLToRepo,
		OnlyMirrorProtectedBranches:      new(true),
		Enabled:                          new(true),
		MirrorOverwritesDivergedBranches: new(true),
		MirrorTriggerBuilds:              new(desiredMirrorTriggerBuilds),
	})
	if err != nil {
		return fmt.Errorf("failed to configure pull mirror for project %s: %w", destinationProject.PathWithNamespace, err)
	}

	return nil
}

// CopyProjectAvatar copies the avatar from the source project to the destination project.
// It first checks if the destination project already has an avatar set. If not, it downloads the avatar from the source project
// and uploads it to the destination project.
// The avatar is saved with a unique filename based on the current timestamp.
// The function returns an error if any step fails, including downloading or uploading the avatar.
func (g *GitlabInstance) CopyProjectAvatar(destinationGitlabInstance *GitlabInstance, destinationProject, sourceProject *gitlab.Project) error {
	zap.L().Debug("Checking if project avatar is already set", zap.String("project", destinationProject.HTTPURLToRepo))

	// Check if the destination project already has an avatar
	if destinationProject.AvatarURL != "" {
		zap.L().Debug("Project already has an avatar set, skipping.", zap.String("project", destinationProject.HTTPURLToRepo), zap.String("path", destinationProject.AvatarURL))

		return nil
	}

	// Nothing to copy when the source project has no avatar: DownloadAvatar would
	// answer 404 and report a failure for every single avatar-less project, drowning
	// out the errors that actually matter.
	if sourceProject.AvatarURL == "" {
		zap.L().Debug("Source project has no avatar, skipping.", zap.String(RoleSource, sourceProject.HTTPURLToRepo))

		return nil
	}

	zap.L().Debug("Copying project avatar", zap.String(RoleSource, sourceProject.HTTPURLToRepo), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

	// Download the source project avatar
	sourceProjectAvatar, _, err := g.Gitlab.Projects.DownloadAvatar(sourceProject.ID)
	if err != nil {
		return fmt.Errorf("failed to download avatar for project %s: %w", sourceProject.HTTPURLToRepo, err)
	}

	// Upload the avatar to the destination project
	_, _, err = destinationGitlabInstance.Gitlab.Projects.UploadAvatar(destinationProject.ID, sourceProjectAvatar, fmt.Sprintf("avatar-%d.png", time.Now().Unix()))
	if err != nil {
		return fmt.Errorf("failed to upload avatar for project %s: %w", destinationProject.HTTPURLToRepo, err)
	}

	return nil
}

// AddProjectToCICDCatalog enables the CI/CD catalog resource for the project in the destination GitLab instance.
// It skips the API call if the project is already registered as a CI/CD catalog resource.
// Requires GitLab 19.3+ on the destination instance, since it relies on the "cicd_catalog_enabled" project API field introduced in that version.
func (g *GitlabInstance) AddProjectToCICDCatalog(project *gitlab.Project) error {
	if project.CICDCatalogEnabled {
		zap.L().Debug("Project is already part of the CI/CD catalog, skipping", zap.String("project", project.HTTPURLToRepo))

		return nil
	}

	zap.L().Debug("Adding project to CI/CD catalog", zap.String("project", project.HTTPURLToRepo))

	_, _, err := g.Gitlab.Projects.EditProject(project.ID, &gitlab.EditProjectOptions{CICDCatalogEnabled: new(true)})
	if err != nil {
		return fmt.Errorf("failed to add project %s to CI/CD catalog: %w", project.PathWithNamespace, err)
	}

	return nil
}

// ClaimOwnershipToProject ensures the authenticated user is a direct owner of the specified project.
//
// It is called on every run, so it must actively reassert ownership rather than silently
// no-op: AddProjectMember does not change the access level of a user who is already a
// member, so a user added at a lower level (or by a previous run under a different account)
// would otherwise never be promoted to Owner. It tries EditProjectMember first to upgrade an
// existing direct membership, falling back to AddProjectMember when the user isn't a direct
// member yet.
func (g *GitlabInstance) ClaimOwnershipToProject(project *gitlab.Project) error {
	zap.L().Debug("Claiming ownership of project", zap.String("project", project.PathWithNamespace), zap.Int64("userID", g.UserID))

	ownerAccessLevel := new(gitlab.AccessLevelValue(projectOwnerAccessLevel))

	_, _, editErr := g.Gitlab.ProjectMembers.EditProjectMember(project.ID, g.UserID, &gitlab.EditProjectMemberOptions{
		AccessLevel: ownerAccessLevel,
	})
	if editErr != nil {
		_, _, addErr := g.Gitlab.ProjectMembers.AddProjectMember(project.ID, &gitlab.AddProjectMemberOptions{
			UserID:      &g.UserID,
			AccessLevel: ownerAccessLevel,
		})
		if addErr != nil {
			return fmt.Errorf("failed to add or promote user as owner of project %s: %w", project.PathWithNamespace, errors.Join(editErr, addErr))
		}
	}

	zap.L().Info("Successfully claimed ownership of project", zap.String("project", project.PathWithNamespace))

	return nil
}
