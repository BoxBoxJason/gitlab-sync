package mirroring

import (
	"fmt"
	"sync"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"
)

// creationMode tells how the entities of a project are created.
type creationMode int

const (
	// createConcurrently starts one goroutine per entity.
	createConcurrently creationMode = iota
	// createSequentially creates the entities one at a time, in source order.
	createSequentially
)

// mirrorProjectEntities mirrors a category of project entities (issues, releases,
// ...) from a source project to a destination project. With createSequentially the
// entities are created one at a time, in source order, instead of concurrently. Every failure is logged as
// it happens via helpers.Report.
func mirrorProjectEntities[T any](
	entityName string,
	sourceProject *gitlab.Project,
	destinationProject *gitlab.Project,
	fetchExisting func(*gitlab.Project) (map[string]struct{}, error),
	fetchSource func(*gitlab.Project) ([]T, error),
	getKey func(T) string,
	createEntity func(*gitlab.Project, T) error,
	mode creationMode,
) {
	zap.L().Info("Starting "+entityName+" mirroring", zap.String(RoleSource, sourceProject.HTTPURLToRepo), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

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

	create := func(entity T, key string) {
		createErr := createEntity(destinationProject, entity)
		if createErr != nil {
			helpers.Report(fmt.Errorf("failed to create %s %s in project %s: %w", entityName, key, destinationProject.HTTPURLToRepo, createErr))
		}
	}

	for _, sourceEntity := range sourceEntities {
		entityKey := getKey(sourceEntity)
		if _, exists := existingKeys[entityKey]; exists {
			zap.L().Debug(entityName+" already exists", zap.String(entityName, entityKey), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

			continue
		}

		if mode == createSequentially {
			create(sourceEntity, entityKey)

			continue
		}

		waitGroup.Add(1)

		go func(entity T, key string) {
			defer waitGroup.Done()

			create(entity, key)
		}(sourceEntity, entityKey)
	}

	waitGroup.Wait()

	zap.L().Info(entityName+" mirroring completed", zap.String(RoleSource, sourceProject.HTTPURLToRepo), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))
}
