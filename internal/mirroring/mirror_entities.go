package mirroring

import (
	"fmt"
	"sync"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
	"go.uber.org/zap"
)

// mirrorProjectEntities mirrors a category of project entities (issues, releases,
// ...) from a source project to a destination project. Every failure is logged as
// it happens via helpers.Report.
func mirrorProjectEntities[T any](
	entityName string,
	sourceProject *gitlab.Project,
	destinationProject *gitlab.Project,
	fetchExisting func(*gitlab.Project) (map[string]struct{}, error),
	fetchSource func(*gitlab.Project) ([]T, error),
	getKey func(T) string,
	createEntity func(*gitlab.Project, T) error,
) {
	zap.L().Info("Starting "+entityName+" mirroring", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

	existingKeys, err := fetchExisting(destinationProject)
	if err != nil {
		helpers.Report(fmt.Errorf("failed to fetch existing %s for destination project %s: %w", entityName, destinationProject.HTTPURLToRepo, err))

		return
	}

	sourceEntities, err := fetchSource(sourceProject)
	if err != nil {
		helpers.Report(fmt.Errorf("failed to fetch %s for source project %s: %w", entityName, sourceProject.HTTPURLToRepo, err))

		return
	}

	var waitGroup sync.WaitGroup

	for _, sourceEntity := range sourceEntities {
		entityKey := getKey(sourceEntity)
		if _, exists := existingKeys[entityKey]; exists {
			zap.L().Debug(entityName+" already exists", zap.String(entityName, entityKey), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

			continue
		}

		waitGroup.Add(1)

		go func(entity T, key string) {
			defer waitGroup.Done()

			createErr := createEntity(destinationProject, entity)
			if createErr != nil {
				helpers.Report(fmt.Errorf("failed to create %s %s in project %s: %w", entityName, key, destinationProject.HTTPURLToRepo, createErr))
			}
		}(sourceEntity, entityKey)
	}

	waitGroup.Wait()

	zap.L().Info(entityName+" mirroring completed", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
}
