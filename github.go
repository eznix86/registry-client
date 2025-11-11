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
	Organization string
	APIToken     string
	api          packagesAPI
}

func NewGitHubClient(token string) *GitHubClient {
	client := &Client{
		BaseURL: "https://ghcr.io",
		Auth:    BearerAuth{Token: token},
	}
	return &GitHubClient{
		Client:   client,
		Type:     GitHubUser,
		APIToken: token,
		api: &githubPackagesAPI{
			client:   client,
			apiToken: token,
			baseURL:  "https://api.github.com",
		},
	}
}

func NewGitHubOrgClient(token string, org string) *GitHubClient {
	client := &Client{
		BaseURL: "https://ghcr.io",
		Auth:    BearerAuth{Token: token},
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

	repositories := make([]string, len(packagesResp.Packages))
	for i, pkg := range packagesResp.Packages {
		repositories[i] = pkg.Name
	}

	return &CatalogResponse{
		Repositories:      repositories,
		PaginatedResponse: packagesResp.PaginatedResponse,
	}, nil
}

func (api *githubPackagesAPI) getUserPackages(ctx context.Context, pagination *PaginationParams) (*GitHubPackagesResponse, error) {
	apiURL := api.baseURL + "/user/packages"

	logArgs := []any{
		"operation", "getUserPackages",
		"method", http.MethodGet,
		"url", apiURL,
	}
	if pagination != nil {
		logArgs = append(logArgs, "page_size", pagination.N, "last", pagination.Last)
	}
	api.client.logDebug("GitHub API request", logArgs...)

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

	req.Header.Set("Authorization", "Bearer "+api.apiToken)

	resp, err := api.client.Do(req)
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

	linkHeader := resp.Header.Get("Link")
	paginationResp := parseGitHubLinkHeader(linkHeader)

	api.client.logDebug("GitHub API response",
		"operation", "getUserPackages",
		"package_count", len(packages),
		"has_more", paginationResp.HasMore,
	)

	return &GitHubPackagesResponse{
		Packages:          packages,
		PaginatedResponse: paginationResp,
	}, nil
}

func (api *githubPackagesAPI) getOrgPackages(ctx context.Context, org string, pagination *PaginationParams) (*GitHubPackagesResponse, error) {
	apiURL := fmt.Sprintf("%s/orgs/%s/packages", api.baseURL, org)

	logArgs := []any{
		"operation", "getOrgPackages",
		"method", http.MethodGet,
		"organization", org,
		"url", apiURL,
	}
	if pagination != nil {
		logArgs = append(logArgs, "page_size", pagination.N, "last", pagination.Last)
	}
	api.client.logDebug("GitHub API request", logArgs...)

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

	req.Header.Set("Authorization", "Bearer "+api.apiToken)

	resp, err := api.client.Do(req)
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

	linkHeader := resp.Header.Get("Link")
	paginationResp := parseGitHubLinkHeader(linkHeader)

	api.client.logDebug("GitHub API response",
		"operation", "getOrgPackages",
		"organization", org,
		"package_count", len(packages),
		"has_more", paginationResp.HasMore,
	)

	return &GitHubPackagesResponse{
		Packages:          packages,
		PaginatedResponse: paginationResp,
	}, nil
}

func parseGitHubLinkHeader(linkHeader string) PaginatedResponse {
	if linkHeader == "" {
		return PaginatedResponse{}
	}

	// Split by comma to get individual link entries
	links := strings.Split(linkHeader, ",")

	var nextPage string
	var pageSize int
	hasNext := false

	for _, link := range links {
		// Check if this is the "next" relation
		if strings.Contains(link, `rel="next"`) {
			hasNext = true

			// Extract URL from <...>
			start := strings.Index(link, "<")
			end := strings.Index(link, ">")
			if start != -1 && end != -1 && end > start {
				linkURL := link[start+1 : end]

				// Parse URL to get page and per_page parameters
				parsedURL, err := url.Parse(linkURL)
				if err == nil {
					nextPage = parsedURL.Query().Get("page")
					perPage := parsedURL.Query().Get("per_page")
					if perPage != "" {
						_, _ = fmt.Sscanf(perPage, "%d", &pageSize)
					}
				}
			}
			break
		}
	}

	return PaginatedResponse{
		HasMore: hasNext,
		Last:    nextPage,
		N:       pageSize,
	}
}
