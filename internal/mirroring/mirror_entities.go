package mirroring

import (
	"fmt"
	"sync"

	"gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

func mirrorProjectEntities[T any](
	entityName string,
	sourceProject *gitlab.Project,
	destinationProject *gitlab.Project,
	fetchExisting func(*gitlab.Project) (map[string]struct{}, error),
	fetchSource func(*gitlab.Project) ([]T, error),
	getKey func(T) string,
	createEntity func(*gitlab.Project, T) error,
) []error {
	zap.L().Info("Starting "+entityName+" mirroring", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

	existingKeys, err := fetchExisting(destinationProject)
	if err != nil {
		return []error{fmt.Errorf("failed to fetch existing %s for destination project %s: %w", entityName, destinationProject.HTTPURLToRepo, err)}
	}

	sourceEntities, err := fetchSource(sourceProject)
	if err != nil {
		return []error{fmt.Errorf("failed to fetch %s for source project %s: %w", entityName, sourceProject.HTTPURLToRepo, err)}
	}

	var (
		waitGroup    sync.WaitGroup
		errorChannel = make(chan error, len(sourceEntities))
	)

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
				errorChannel <- fmt.Errorf("failed to create %s %s in project %s: %w", entityName, key, destinationProject.HTTPURLToRepo, createErr)
			}
		}(sourceEntity, entityKey)
	}

	waitGroup.Wait()
	close(errorChannel)

	zap.L().Info(entityName+" mirroring completed", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

	return helpers.MergeErrors(errorChannel)
}
