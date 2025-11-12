package registryclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// GitHubClientType represents whether the client is for a user or organization
type GitHubClientType string

const (
	GitHubUser GitHubClientType = "user"
	GitHubOrg  GitHubClientType = "org"
)

type packagesAPI interface {
	getUserPackages(ctx context.Context, pagination *PaginationParams) (*GitHubPackagesResponse, error)
	getOrgPackages(ctx context.Context, org string, pagination *PaginationParams) (*GitHubPackagesResponse, error)
}

type githubPackagesAPI struct {
	client   *Client
	apiToken string
	baseURL  string
}

type GitHubClient struct {
	*Client
	Type         GitHubClientType
	Username     string // GitHub username for user client
	Organization string // GitHub organization for org client
	APIToken     string
	api          packagesAPI
}

func NewGitHubClient(username, token string) *GitHubClient {
	encodedToken := base64.StdEncoding.EncodeToString([]byte(token))
	client := &Client{
		BaseURL: "https://ghcr.io",
		Auth:    BearerAuth{Token: encodedToken},
	}
	return &GitHubClient{
		Client:   client,
		Type:     GitHubUser,
		Username: username,
		APIToken: token,
		api: &githubPackagesAPI{
			client:   client,
			apiToken: token,
			baseURL:  "https://api.github.com",
		},
	}
}

func NewGitHubOrgClient(org, token string) *GitHubClient {
	encodedToken := base64.StdEncoding.EncodeToString([]byte(token))
	client := &Client{
		BaseURL: "https://ghcr.io",
		Auth:    BearerAuth{Token: encodedToken},
	}
	return &GitHubClient{
		Client:       client,
		Type:         GitHubOrg,
		Organization: org,
		APIToken:     token,
		api: &githubPackagesAPI{
			client:   client,
			apiToken: token,
			baseURL:  "https://api.github.com",
		},
	}
}

func (gc *GitHubClient) GetCatalog(ctx context.Context, pagination *PaginationParams) (*CatalogResponse, error) {
	var packagesResp *GitHubPackagesResponse
	var err error

	if gc.Type == GitHubOrg {
		packagesResp, err = gc.api.getOrgPackages(ctx, gc.Organization, pagination)
	} else {
		packagesResp, err = gc.api.getUserPackages(ctx, pagination)
	}

	if err != nil {
		return nil, err
	}

	// Prefix package names with username/org for ghcr.io compatibility
	prefix := gc.Username
	if gc.Type == GitHubOrg {
		prefix = gc.Organization
	}

	repositories := make([]string, len(packagesResp.Packages))
	for i, pkg := range packagesResp.Packages {
		repositories[i] = prefix + "/" + pkg.Name
	}

	return &CatalogResponse{
		Repositories:      repositories,
		PaginatedResponse: packagesResp.PaginatedResponse,
	}, nil
}

// DeleteManifest deletes a manifest by finding its package version and deleting it.
// reference can be either a tag name (e.g., "latest", "v1.2.3") or a digest (e.g., "sha256:abc123...").
// This overrides the standard registry DeleteManifest which doesn't work on GitHub Container Registry.
// The acceptHeaders parameter is ignored for GitHub Container Registry.
func (gc *GitHubClient) DeleteManifest(ctx context.Context, repository, reference string, acceptHeaders ...string) error {
	// For GitHub packages, extract package name after the first '/'
	// e.g., "eznix86/textbee/api" -> "textbee/api"
	idx := strings.Index(repository, "/")
	packageName := repository
	if idx != -1 {
		packageName = repository[idx+1:]
	}

	gc.logDebug("GitHub delete manifest",
		"operation", "DeleteManifest",
		"repository", repository,
		"package", packageName,
		"reference", reference,
	)

	versionID, err := gc.findPackageVersionID(ctx, packageName, reference)
	if err != nil {
		return err
	}

	if err := gc.deletePackageVersion(ctx, packageName, versionID); err != nil {
		return err
	}

	gc.logDebug("GitHub delete manifest success",
		"operation", "DeleteManifest",
		"repository", repository,
		"reference", reference,
		"version_id", versionID,
	)

	return nil
}

func buildGitHubPackagesRequest(ctx context.Context, apiURL, token string, pagination *PaginationParams) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("package_type", "container")

	if pagination != nil {
		if pagination.N > 0 {
			q.Add("per_page", fmt.Sprintf("%d", pagination.N))
		}
		if pagination.Last != "" {
			q.Add("page", pagination.Last)
		}
	}

	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}

func (api *githubPackagesAPI) getUserPackages(ctx context.Context, pagination *PaginationParams) (*GitHubPackagesResponse, error) {
	apiURL := api.baseURL + "/user/packages"

	logArgs := []any{"operation", "getUserPackages", "method", http.MethodGet, "url", apiURL}
	if pagination != nil {
		logArgs = append(logArgs, "page_size", pagination.N, "last", pagination.Last)
	}
	api.client.logDebug("GitHub API request", logArgs...)

	req, err := buildGitHubPackagesRequest(ctx, apiURL, api.apiToken, pagination)
	if err != nil {
		return nil, err
	}

	// Use http.Client.Do directly to avoid applying the registry auth (base64-encoded token)
	// The buildGitHubPackagesRequest already set the correct Authorization header
	resp, err := api.client.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer api.client.closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get github user packages failed: %s - %s", resp.Status, string(body))
	}

	var packages []GitHubPackage
	if err := json.NewDecoder(resp.Body).Decode(&packages); err != nil {
		return nil, err
	}

	paginationResp := parseGitHubLinkHeader(resp.Header.Get("Link"))
	api.client.logDebug("GitHub API response", "operation", "getUserPackages", "package_count", len(packages), "has_more", paginationResp.HasMore)

	return &GitHubPackagesResponse{
		Packages:          packages,
		PaginatedResponse: paginationResp,
	}, nil
}

func (api *githubPackagesAPI) getOrgPackages(ctx context.Context, org string, pagination *PaginationParams) (*GitHubPackagesResponse, error) {
	apiURL := fmt.Sprintf("%s/orgs/%s/packages", api.baseURL, org)

	logArgs := []any{"operation", "getOrgPackages", "method", http.MethodGet, "organization", org, "url", apiURL}
	if pagination != nil {
		logArgs = append(logArgs, "page_size", pagination.N, "last", pagination.Last)
	}
	api.client.logDebug("GitHub API request", logArgs...)

	req, err := buildGitHubPackagesRequest(ctx, apiURL, api.apiToken, pagination)
	if err != nil {
		return nil, err
	}

	// Use http.Client.Do directly to avoid applying the registry auth (base64-encoded token)
	// The buildGitHubPackagesRequest already set the correct Authorization header
	resp, err := api.client.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer api.client.closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get github org packages failed: %s - %s", resp.Status, string(body))
	}

	var packages []GitHubPackage
	if err := json.NewDecoder(resp.Body).Decode(&packages); err != nil {
		return nil, err
	}

	paginationResp := parseGitHubLinkHeader(resp.Header.Get("Link"))
	api.client.logDebug("GitHub API response", "operation", "getOrgPackages", "organization", org, "package_count", len(packages), "has_more", paginationResp.HasMore)

	return &GitHubPackagesResponse{
		Packages:          packages,
		PaginatedResponse: paginationResp,
	}, nil
}

func buildPackageVersionsURL(baseURL string, clientType GitHubClientType, org, packageName string) (string, error) {
	escapedPkg := url.PathEscape(packageName)
	var path string
	if clientType == GitHubOrg {
		path = fmt.Sprintf("/orgs/%s/packages/container/%s/versions", org, escapedPkg)
	} else {
		path = fmt.Sprintf("/user/packages/container/%s/versions", escapedPkg)
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	parsedURL.Path = path
	parsedURL.RawPath = path // Preserve percent encoding
	return parsedURL.String(), nil
}

func buildPackageVersionURL(baseURL string, clientType GitHubClientType, org, packageName string, versionID int) (string, error) {
	escapedPkg := url.PathEscape(packageName)
	var path string
	if clientType == GitHubOrg {
		path = fmt.Sprintf("/orgs/%s/packages/container/%s/versions/%d", org, escapedPkg, versionID)
	} else {
		path = fmt.Sprintf("/user/packages/container/%s/versions/%d", escapedPkg, versionID)
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	parsedURL.Path = path
	parsedURL.RawPath = path // Preserve percent encoding
	return parsedURL.String(), nil
}

func (gc *GitHubClient) listPackageVersions(ctx context.Context, packageName string, pagination *PaginationParams) ([]GitHubPackageVersion, error) {
	baseURL := gc.api.(*githubPackagesAPI).baseURL
	apiURL, err := buildPackageVersionsURL(baseURL, gc.Type, gc.Organization, packageName)
	if err != nil {
		return nil, err
	}

	logArgs := []any{"operation", "listPackageVersions", "method", http.MethodGet, "package", packageName, "url", apiURL}
	if pagination != nil {
		logArgs = append(logArgs, "page_size", pagination.N, "page", pagination.Last)
	}
	gc.logDebug("GitHub API request", logArgs...)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("state", "active")
	if pagination != nil {
		if pagination.N > 0 {
			q.Add("per_page", fmt.Sprintf("%d", pagination.N))
		}
		if pagination.Last != "" {
			q.Add("page", pagination.Last)
		}
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+gc.APIToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Use http.Client.Do directly to avoid applying the registry auth (base64-encoded token)
	// The Authorization header was already set with the correct raw token
	//nolint:staticcheck // QF1008: Intentionally using Client.Do to bypass Auth.Apply
	resp, err := gc.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer gc.closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list package versions failed: %s - %s", resp.Status, string(body))
	}

	var versions []GitHubPackageVersion
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, err
	}

	gc.logDebug("GitHub API response", "operation", "listPackageVersions", "package", packageName, "version_count", len(versions))
	return versions, nil
}

//nolint:funlen // complex pagination and search logic
func (gc *GitHubClient) findPackageVersionID(ctx context.Context, packageName, reference string) (int, error) {
	isDigest := strings.HasPrefix(reference, "sha256:")
	page := 1

	for {
		versions, err := gc.listPackageVersions(ctx, packageName, &PaginationParams{N: 100, Last: fmt.Sprintf("%d", page)})
		if err != nil {
			return 0, err
		}

		if len(versions) == 0 {
			break
		}

		for _, v := range versions {
			if isDigest {
				if v.Name == reference {
					gc.logDebug("Found package version by digest", "package", packageName, "reference", reference, "version_id", v.ID)
					return v.ID, nil
				}
			} else {
				if slices.Contains(v.Metadata.Container.Tags, reference) {
					gc.logDebug("Found package version by tag", "package", packageName, "reference", reference, "version_id", v.ID)
					return v.ID, nil
				}
			}
		}

		if len(versions) < 100 {
			break
		}
		page++
	}

	return 0, fmt.Errorf("package version not found for reference: %s", reference)
}

func (gc *GitHubClient) deletePackageVersion(ctx context.Context, packageName string, versionID int) error {
	baseURL := gc.api.(*githubPackagesAPI).baseURL
	apiURL, err := buildPackageVersionURL(baseURL, gc.Type, gc.Organization, packageName, versionID)
	if err != nil {
		return err
	}

	gc.logDebug("GitHub API request", "operation", "deletePackageVersion", "method", http.MethodDelete, "package", packageName, "version_id", versionID, "url", apiURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+gc.APIToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Use http.Client.Do directly to avoid applying the registry auth (base64-encoded token)
	// The Authorization header was already set with the correct raw token
	//nolint:staticcheck // QF1008: Intentionally using Client.Do to bypass Auth.Apply
	resp, err := gc.Client.Do(req)
	if err != nil {
		return err
	}
	defer gc.closeBody(resp.Body)

	switch resp.StatusCode {
	case http.StatusNoContent:
		gc.logDebug("GitHub API response", "operation", "deletePackageVersion", "package", packageName, "version_id", versionID, "status", "success")
		return nil
	case http.StatusForbidden:
		return fmt.Errorf("cannot delete package version: insufficient permissions or package has >5,000 downloads")
	case http.StatusNotFound:
		return fmt.Errorf("package version not found")
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete package version failed: %s - %s", resp.Status, string(body))
	}
}

func parseGitHubLinkURL(linkURL string) (page string, pageSize int) {
	parsedURL, err := url.Parse(linkURL)
	if err != nil {
		return "", 0
	}

	page = parsedURL.Query().Get("page")
	perPage := parsedURL.Query().Get("per_page")
	if perPage != "" {
		_, _ = fmt.Sscanf(perPage, "%d", &pageSize)
	}
	return page, pageSize
}

func extractLinkURL(link string) string {
	start := strings.Index(link, "<")
	end := strings.Index(link, ">")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return link[start+1 : end]
}

func parseGitHubLinkHeader(linkHeader string) PaginatedResponse {
	if linkHeader == "" {
		return PaginatedResponse{}
	}

	links := strings.Split(linkHeader, ",")

	for _, link := range links {
		if !strings.Contains(link, `rel="next"`) {
			continue
		}

		linkURL := extractLinkURL(link)
		if linkURL == "" {
			return PaginatedResponse{HasMore: true}
		}

		nextPage, pageSize := parseGitHubLinkURL(linkURL)
		return PaginatedResponse{
			HasMore: true,
			Last:    nextPage,
			N:       pageSize,
		}
	}

	return PaginatedResponse{}
}
