// Package mirroring mirrors GitLab groups, projects, releases and issues between instances.
package mirroring

const (
	// RoleKey is the logging key for GitLab instance role.
	RoleKey = "role"
	// RoleSource is the source GitLab instance role value.
	RoleSource = "source"
	// RoleDestination is the destination GitLab instance role value.
	RoleDestination = "destination"

	// InstanceSizeKey is the logging key for GitLab instance size.
	InstanceSizeKey = "size"
	// InstanceSizeSmall is the identifier for small instances.
	InstanceSizeSmall = "small"
	// InstanceSizeBig is the identifier for big instances.
	InstanceSizeBig = "big"
)
