package registryclient

import "context"

// RegistryClient defines the common interface for interacting with container registries.
// Both standard OCI/Docker registries and GitHub Container Registry implement this interface.
type RegistryClient interface {
	// HealthCheck verifies registry availability.
	// Returns HTTP status code and error only for programming errors.
	HealthCheck(ctx context.Context) (int, error)

	// GetCatalog retrieves the list of repositories.
	// Supports optional pagination.
	GetCatalog(ctx context.Context, pagination *PaginationParams) (*CatalogResponse, error)

	// GetManifest retrieves a manifest by repository and reference.
	// Optional acceptHeaders can override defaults.
	GetManifest(ctx context.Context, repository, reference string, acceptHeaders ...string) (*ManifestResponse, error)

	// HasManifest checks whether a manifest exists.
	HasManifest(ctx context.Context, repository, reference string, acceptHeaders ...string) (bool, error)

	// GetBlob fetches a blob by digest.
	GetBlob(ctx context.Context, repository, digest string) (*BlobResponse, error)

	// HasBlob checks if a blob exists.
	HasBlob(ctx context.Context, repository, digest string) (bool, error)

	// ListTags retrieves all tags for a repository.
	// Supports optional pagination.
	ListTags(ctx context.Context, repository string, pagination *PaginationParams) (*TagsResponse, error)

	// DeleteManifest deletes a manifest.
	// For standard registries: reference must be a digest (sha256:...)
	// For GitHub: reference can be a tag or digest
	DeleteManifest(ctx context.Context, repository, reference string, acceptHeaders ...string) error
}

// Compile-time interface compliance checks
var (
	_ RegistryClient = (*BaseClient)(nil)
	_ RegistryClient = (*GitHubClient)(nil)
)
