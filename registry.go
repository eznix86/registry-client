package registryclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var defaultManifestMediaTypes = []string{
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.v2+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
}

// ParseConfigBlob parses a config blob's content into a structured ConfigBlob.
func ParseConfigBlob(content []byte) (*ConfigBlob, error) {
	var cfg ConfigBlob
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}

	m.Raw = b

	switch m.MediaType {
	case "application/vnd.oci.image.manifest.v1+json":
		fallthrough

	case "application/vnd.docker.distribution.manifest.v2+json":
		var img ImageManifest
		if err := json.Unmarshal(b, &img); err != nil {
			return nil, err
		}
		m.ManifestData = img

	case "application/vnd.oci.image.index.v1+json":
		fallthrough
	case "application/vnd.docker.distribution.manifest.list.v2+json":
		var list ManifestList
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, err
		}
		m.ManifestData = list

	default:
		return nil, fmt.Errorf("unsupported mediaType: %s", m.MediaType)
	}

	return &m, nil
}

// addAcceptHeaders adds Accept headers for OCI/Docker manifests.
// If customHeaders is provided, only those are used.
func addAcceptHeaders(req *http.Request, customHeaders []string) {
	headers := defaultManifestMediaTypes
	if len(customHeaders) > 0 {
		headers = customHeaders
	}
	for _, h := range headers {
		req.Header.Add("Accept", h)
	}
}

// parseLinkHeader parses the Link header and extracts pagination parameters.
// Link format: </v2/_catalog?last=repo&n=100>; rel="next"
func parseLinkHeader(linkHeader string) PaginatedResponse {
	if linkHeader == "" {
		return PaginatedResponse{}
	}

	// Parse the link header
	parts := strings.Split(linkHeader, ";")
	if len(parts) < 1 {
		return PaginatedResponse{}
	}

	// Extract URL from <...>
	urlPart := strings.TrimSpace(parts[0])
	urlPart = strings.Trim(urlPart, "<>")

	// Parse URL to get query parameters
	parsedURL, err := url.Parse(urlPart)
	if err != nil {
		return PaginatedResponse{}
	}

	query := parsedURL.Query()
	last := query.Get("last")

	// Parse n parameter if present
	var n int
	nStr := query.Get("n")
	if nStr != "" {
		_, _ = fmt.Sscanf(nStr, "%d", &n) // Ignore scan errors, n remains 0
	}

	return PaginatedResponse{
		HasMore: true,
		Last:    last,
		N:       n,
	}
}

// applyPagination adds pagination query parameters to the request if provided
func applyPagination(req *http.Request, pagination *PaginationParams) {
	if pagination == nil {
		return
	}
	q := req.URL.Query()
	if pagination.N > 0 {
		q.Add("n", fmt.Sprintf("%d", pagination.N))
	}
	if pagination.Last != "" {
		q.Add("last", pagination.Last)
	}
	req.URL.RawQuery = q.Encode()
}

// HealthCheck performs a GET on /v2/ to verify registry availability.
func (c *Client) HealthCheck(ctx context.Context) (*int, error) {
	url := fmt.Sprintf("%s/v2/", c.BaseURL)

	c.logDebug("Registry request",
		"operation", "HealthCheck",
		"method", http.MethodGet,
		"url", url,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer c.closeBody(resp.Body)

	c.logDebug("Registry response",
		"operation", "HealthCheck",
		"status_code", resp.StatusCode,
	)

	return &resp.StatusCode, nil
}

// GetCatalog retrieves the list of repositories from /v2/_catalog.
// Optional pagination parameters can be provided.
func (c *Client) GetCatalog(ctx context.Context, pagination *PaginationParams) (*CatalogResponse, error) {
	url := fmt.Sprintf("%s/v2/_catalog", c.BaseURL)

	logArgs := []any{
		"operation", "GetCatalog",
		"method", http.MethodGet,
		"url", url,
	}
	if pagination != nil {
		logArgs = append(logArgs, "page_size", pagination.N, "last", pagination.Last)
	}
	c.logDebug("Registry request", logArgs...)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	applyPagination(req, pagination)

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer c.closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get catalog failed: %s - %s", resp.Status, string(body))
	}

	var data struct {
		Repositories []string `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	linkHeader := resp.Header.Get("Link")
	paginationResp := parseLinkHeader(linkHeader)

	c.logDebug("Registry response",
		"operation", "GetCatalog",
		"repository_count", len(data.Repositories),
		"has_more", paginationResp.HasMore,
	)

	return &CatalogResponse{
		Repositories:      data.Repositories,
		PaginatedResponse: paginationResp,
	}, nil
}

// GetManifest retrieves a manifest by repository and reference.
// Optional acceptHeaders can override defaults.
func (c *Client) GetManifest(ctx context.Context, repository, reference string, acceptHeaders ...string) (*ManifestResponse, error) {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", c.BaseURL, repository, reference)

	c.logDebug("Registry request",
		"operation", "GetManifest",
		"method", http.MethodGet,
		"repository", repository,
		"reference", reference,
		"url", url,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	addAcceptHeaders(req, acceptHeaders)

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer c.closeBody(resp.Body)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get manifest failed: %s - %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	manifest, err := ParseManifest(body)
	if err != nil {
		return nil, err
	}

	c.logDebug("Registry response",
		"operation", "GetManifest",
		"repository", repository,
		"reference", reference,
		"media_type", manifest.MediaType,
		"digest", resp.Header.Get("Docker-Content-Digest"),
		"schema_version", manifest.SchemaVersion,
	)

	return &ManifestResponse{
		SchemaVersion: manifest.SchemaVersion,
		MediaType:     manifest.MediaType,
		ManifestData:  manifest.ManifestData,
		Digest:        resp.Header.Get("Docker-Content-Digest"),
		RawContent:    body,
	}, nil
}

// HasManifest checks whether a manifest exists for a repository/reference.
func (c *Client) HasManifest(ctx context.Context, repository, reference string, acceptHeaders ...string) (bool, error) {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", c.BaseURL, repository, reference)

	c.logDebug("Registry request",
		"operation", "HasManifest",
		"method", http.MethodHead,
		"repository", repository,
		"reference", reference,
		"url", url,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false, err
	}

	addAcceptHeaders(req, acceptHeaders)

	resp, err := c.Do(req)
	if err != nil {
		return false, err
	}
	defer c.closeBody(resp.Body)

	exists := resp.StatusCode == http.StatusOK
	c.logDebug("Registry response",
		"operation", "HasManifest",
		"repository", repository,
		"reference", reference,
		"exists", exists,
		"status_code", resp.StatusCode,
	)

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected status: %s", resp.Status)
	}
}

// GetBlob fetches a blob
func (c *Client) GetBlob(ctx context.Context, repository, digest string) (*BlobResponse, error) {
	url := fmt.Sprintf("%s/v2/%s/blobs/%s", c.BaseURL, repository, digest)

	c.logDebug("Registry request",
		"operation", "GetBlob",
		"method", http.MethodGet,
		"repository", repository,
		"digest", digest,
		"url", url,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer c.closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get blob failed: %s - %s", resp.Status, string(body))
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	c.logDebug("Registry response",
		"operation", "GetBlob",
		"repository", repository,
		"digest", digest,
		"size_bytes", len(content),
	)

	return &BlobResponse{
		Digest:  resp.Header.Get("Docker-Content-Digest"),
		Content: content,
		Size:    int64(len(content)),
	}, nil
}

// ListTags retrieves all tags for a given repository.
// Optional pagination parameters can be provided.
func (c *Client) ListTags(ctx context.Context, repository string, pagination *PaginationParams) (*TagsResponse, error) {
	url := fmt.Sprintf("%s/v2/%s/tags/list", c.BaseURL, repository)

	logArgs := []any{
		"operation", "ListTags",
		"method", http.MethodGet,
		"repository", repository,
		"url", url,
	}
	if pagination != nil {
		logArgs = append(logArgs, "page_size", pagination.N, "last", pagination.Last)
	}
	c.logDebug("Registry request", logArgs...)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	applyPagination(req, pagination)

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer c.closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list tags failed: %s - %s", resp.Status, string(body))
	}

	var data struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	linkHeader := resp.Header.Get("Link")
	paginationResp := parseLinkHeader(linkHeader)

	c.logDebug("Registry response",
		"operation", "ListTags",
		"repository", repository,
		"tag_count", len(data.Tags),
		"has_more", paginationResp.HasMore,
	)

	return &TagsResponse{
		Name:              data.Name,
		Tags:              data.Tags,
		PaginatedResponse: paginationResp,
	}, nil
}

// DeleteManifest deletes a manifest by repository and digest.
// Note: reference must be a digest (sha256:...), not a tag.
// Optional acceptHeaders can override defaults.
func (c *Client) DeleteManifest(ctx context.Context, repository, digest string, acceptHeaders ...string) error {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", c.BaseURL, repository, digest)

	c.logDebug("Registry request",
		"operation", "DeleteManifest",
		"method", http.MethodDelete,
		"repository", repository,
		"digest", digest,
		"url", url,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	addAcceptHeaders(req, acceptHeaders)

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer c.closeBody(resp.Body)

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete manifest failed: %s - %s", resp.Status, string(body))
	}

	c.logDebug("Registry response",
		"operation", "DeleteManifest",
		"repository", repository,
		"digest", digest,
		"status_code", resp.StatusCode,
	)

	return nil
}

// HasBlob checks if a blob exists in the repository.
func (c *Client) HasBlob(ctx context.Context, repository, digest string) (bool, error) {
	url := fmt.Sprintf("%s/v2/%s/blobs/%s", c.BaseURL, repository, digest)

	c.logDebug("Registry request",
		"operation", "HasBlob",
		"method", http.MethodHead,
		"repository", repository,
		"digest", digest,
		"url", url,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false, err
	}

	resp, err := c.Do(req)
	if err != nil {
		return false, err
	}
	defer c.closeBody(resp.Body)

	exists := resp.StatusCode == http.StatusOK
	c.logDebug("Registry response",
		"operation", "HasBlob",
		"repository", repository,
		"digest", digest,
		"exists", exists,
		"status_code", resp.StatusCode,
	)

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected status: %s", resp.Status)
	}
}
