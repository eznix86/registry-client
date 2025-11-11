package registryclient

// PaginationParams contains parameters for paginated requests
type PaginationParams struct {
	N    int    // Page size (0 for no limit)
	Last string // Last item from previous page
}

// PaginatedResponse provides pagination metadata
type PaginatedResponse struct {
	HasMore bool   // Whether more results are available
	Last    string // Last item in current page (for next request)
	N       int    // Page size from Link header (if present)
}

// CatalogResponse represents the response from catalog endpoints
type CatalogResponse struct {
	Repositories []string
	PaginatedResponse
}

// TagsResponse represents the response from tags endpoints
type TagsResponse struct {
	Name string
	Tags []string
	PaginatedResponse
}

// ManifestResponse represents the response from manifest endpoints
type ManifestResponse struct {
	SchemaVersion int
	MediaType     string
	ManifestData  any // ImageManifest or ManifestList

	// HTTP response metadata
	Digest     string
	RawContent []byte
}

// BlobResponse represents the response from blob endpoints
type BlobResponse struct {
	Digest  string
	Content []byte
	Size    int64
}

// GitHubPackage represents a GitHub container package
type GitHubPackage struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	PackageType string `json:"package_type"`
	Visibility  string `json:"visibility"`
	URL         string `json:"url"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// GitHubPackagesResponse represents the response from GitHub packages endpoint
type GitHubPackagesResponse struct {
	Packages []GitHubPackage
	PaginatedResponse
}

// GitHubPackageVersion represents a GitHub package version
type GitHubPackageVersion struct {
	ID       int                   `json:"id"`
	Name     string                `json:"name"`
	Metadata GitHubPackageMetadata `json:"metadata"`
}

// GitHubPackageMetadata contains package metadata
type GitHubPackageMetadata struct {
	Container GitHubContainerMetadata `json:"container"`
}

// GitHubContainerMetadata contains container-specific metadata
type GitHubContainerMetadata struct {
	Tags []string `json:"tags"`
}
