package mirroring

import (
	"errors"
	"fmt"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

// UpdateProjectFromSource updates the destination project with settings from the source project.
// It enables the project mirror pull, copies the project avatar, and optionally adds the project to the CI/CD catalog.
// It also mirrors releases if the option is set.
// The function uses goroutines to perform these tasks concurrently and waits for all of them to finish.
func (g *GitlabInstance) UpdateProjectFromSource(sourceGitlabInstance *GitlabInstance, sourceProject, destinationProject *gitlab.Project, copyOptions *mirrorcfg.MirroringOptions) {
	// Immediately capture pointers in local variables to avoid any late overrides
	srcProj := sourceProject

	dstProj := destinationProject
	if srcProj == nil || dstProj == nil {
		helpers.Report(errors.New("source or destination project is nil"))

		return
	}

	releasesOptions := g.releasesMirroringOptions(dstProj, copyOptions)

	waitGroup := sync.WaitGroup{}
	waitGroup.Add(updateProjectBaseTasks)

	g.mirrorProjectGitFirst(sourceGitlabInstance, srcProj, dstProj, copyOptions)

	go func(sourceProj, destinationProj *gitlab.Project) {
		defer waitGroup.Done()

		helpers.Report(g.SyncProjectAttributes(sourceProj, destinationProj, copyOptions))
	}(srcProj, dstProj)

	go func(sourceProj, destinationProj *gitlab.Project) {
		defer waitGroup.Done()

		helpers.Report(sourceGitlabInstance.CopyProjectAvatar(g, destinationProj, sourceProj))
	}(srcProj, dstProj)

	enqueueOptionalProjectTasks(g, sourceGitlabInstance, srcProj, dstProj, copyOptions, &waitGroup)

	// Wait for git duplication and the attribute/avatar/optional tasks to finish
	waitGroup.Wait()

	if helpers.Deref(copyOptions.MirrorReleases, false) {
		g.MirrorReleases(sourceGitlabInstance, srcProj, dstProj, releasesOptions)
	}
}

// releasesMirroringOptions prepares the release mirroring of destinationProject. It
// must run before the git mirroring: when the release assets are mirrored, it takes
// the snapshot of the destination releases described in ReleasesMirroringOptions.
// When that snapshot cannot be taken, the releases the git mirroring removes may
// leave files behind, which is reported but does not stop the mirroring.
func (g *GitlabInstance) releasesMirroringOptions(destinationProject *gitlab.Project, copyOptions *mirrorcfg.MirroringOptions) ReleasesMirroringOptions {
	options := ReleasesMirroringOptions{
		MirrorAssets: helpers.Deref(copyOptions.MirrorReleases, false) && helpers.Deref(copyOptions.MirrorReleaseAssets, false),
	}
	if !options.MirrorAssets {
		return options
	}

	releases, err := g.FetchProjectReleases(destinationProject)
	if err != nil {
		helpers.ReportNonBlocking(fmt.Errorf("failed to fetch the releases of destination project %s before the git mirroring, the files of the releases it removes may be left behind: %w", destinationProject.HTTPURLToRepo, err))

		return options
	}

	options.DestinationReleasesBeforeGit = releases

	return options
}

// mirrorProjectGitFirst runs the git side of the mirroring before any project
// attribute is touched, reporting any failure straight away.
//
// The ordering matters: on a freshly created project the repository is still empty
// and GitLab refuses a default_branch pointing at a branch that does not exist yet.
// On a premium destination it also guarantees the pull mirror (and with it the
// import URL) is configured before SyncProjectAttributes asserts mirror=true, which
// GitLab rejects while the import URL is missing.
func (g *GitlabInstance) mirrorProjectGitFirst(
	sourceGitlabInstance *GitlabInstance,
	sourceProject *gitlab.Project,
	destinationProject *gitlab.Project,
	copyOptions *mirrorcfg.MirroringOptions,
) {
	err := g.MirrorProjectGit(sourceGitlabInstance, sourceProject, destinationProject, copyOptions)
	if err == nil {
		return
	}

	helpers.Report(fmt.Errorf("failed to mirror project repository %s -> %s: %w", sourceProject.PathWithNamespace, destinationProject.PathWithNamespace, err))
}

// SyncProjectAttributes updates the destination project with settings from the source project.
// It checks if any diverged project data exists and if so, it overwrites it.
func syncStandardProjectAttributes(sourceProject, destinationProject *gitlab.Project, gitlabEditOptions *gitlab.EditProjectOptions) bool {
	mismatch := false

	if sourceProject.Name != destinationProject.Name {
		gitlabEditOptions.Name = &sourceProject.Name
		mismatch = true
	}

	if sourceProject.Description != destinationProject.Description {
		gitlabEditOptions.Description = &sourceProject.Description
		mismatch = true
	}

	if sourceProject.DefaultBranch != destinationProject.DefaultBranch {
		gitlabEditOptions.DefaultBranch = &sourceProject.DefaultBranch
		mismatch = true
	}

	if !mirrorcfg.StringArraysMatchValues(sourceProject.Topics, destinationProject.Topics) {
		gitlabEditOptions.Topics = &sourceProject.Topics
		mismatch = true
	}

	return mismatch
}

// syncMirrorProjectAttributes aligns the destination project's pull mirror settings.
//
// Pull mirroring is a Premium feature and GitLab rejects `mirror=true` unless the
// project also carries an import URL and a mirror user (see the `if: :mirror?`
// validations on the EE Project model). When pull mirroring is unavailable - either
// the destination is a Free instance or --destination-force-freemium was passed -
// gitlab-sync clones and pushes the repository itself, so the mirror attributes are
// left untouched (callers skip this function); MirrorProjectGit is what turns an existing pull mirror off.
func syncMirrorProjectAttributes(destinationProject *gitlab.Project, copyOptions *mirrorcfg.MirroringOptions, gitlabEditOptions *gitlab.EditProjectOptions) bool {
	mismatch := false

	desiredMirrorTriggerBuilds := copyOptions.TriggerBuilds()
	if desiredMirrorTriggerBuilds != destinationProject.MirrorTriggerBuilds {
		gitlabEditOptions.MirrorTriggerBuilds = new(desiredMirrorTriggerBuilds)
		mismatch = true
	}

	if !destinationProject.MirrorOverwritesDivergedBranches {
		gitlabEditOptions.MirrorOverwritesDivergedBranches = new(true)
		mismatch = true
	}

	if !destinationProject.Mirror {
		gitlabEditOptions.Mirror = new(true)
		mismatch = true
	}

	return mismatch
}

// SyncProjectAttributes updates the destination project attributes when they differ from the source.
func (g *GitlabInstance) SyncProjectAttributes(sourceProject, destinationProject *gitlab.Project, copyOptions *mirrorcfg.MirroringOptions) error {
	zap.L().Debug("Checking if project requires attributes resync", zap.String(RoleSource, sourceProject.HTTPURLToRepo), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

	gitlabEditOptions := &gitlab.EditProjectOptions{}

	missmatched := syncStandardProjectAttributes(sourceProject, destinationProject, gitlabEditOptions)
	if g.PullMirrorAvailable && syncMirrorProjectAttributes(destinationProject, copyOptions, gitlabEditOptions) {
		missmatched = true
	}

	if copyOptions.Visibility != new(string(destinationProject.Visibility)) {
		visibilityValue := mirrorcfg.ConvertVisibility(copyOptions.Visibility)
		gitlabEditOptions.Visibility = &visibilityValue
		missmatched = true
	}

	if missmatched {
		_, _, err := g.Gitlab.Projects.EditProject(destinationProject.ID, gitlabEditOptions)
		if err != nil {
			return fmt.Errorf("failed to edit project %s: %w", destinationProject.HTTPURLToRepo, err)
		}

		zap.L().Debug("Project attributes resync completed", zap.String(RoleSource, sourceProject.HTTPURLToRepo), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))
	} else {
		zap.L().Debug("Project attributes are already in sync, skipping", zap.String(RoleSource, sourceProject.HTTPURLToRepo), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))
	}

	return nil
}
