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
