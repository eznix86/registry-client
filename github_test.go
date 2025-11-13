package registryclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	json "github.com/eznix86/registry-client/jsoncompat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPackagesAPI struct {
	serverURL string
	client    *BaseClient
}

func (m *mockPackagesAPI) getUserPackages(ctx context.Context, pagination *PaginationParams) (*GitHubPackagesResponse, error) {
	apiURL := m.serverURL + "/user/packages"

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
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := m.client.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer m.client.closeBody(resp.Body)

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

	return &GitHubPackagesResponse{
		Packages:          packages,
		PaginatedResponse: paginationResp,
	}, nil
}

func (m *mockPackagesAPI) getOrgPackages(ctx context.Context, org string, pagination *PaginationParams) (*GitHubPackagesResponse, error) {
	apiURL := fmt.Sprintf("%s/orgs/%s/packages", m.serverURL, org)

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
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := m.client.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer m.client.closeBody(resp.Body)

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

	return &GitHubPackagesResponse{
		Packages:          packages,
		PaginatedResponse: paginationResp,
	}, nil
}

func TestNewGitHubClient(t *testing.T) {
	client := NewGitHubClient("testuser", "test-token")

	require.NotNil(t, client)
	assert.Equal(t, GitHubUser, client.Type)
	assert.Equal(t, "https://ghcr.io", client.BaseURL)
	assert.Equal(t, "testuser", client.Username)
	assert.Equal(t, "", client.Organization)
	assert.Equal(t, "test-token", client.APIToken)
}

func TestNewGitHubOrgClient(t *testing.T) {
	client := NewGitHubOrgClient("myorg", "test-token")

	require.NotNil(t, client)
	assert.Equal(t, GitHubOrg, client.Type)
	assert.Equal(t, "https://ghcr.io", client.BaseURL)
	assert.Equal(t, "myorg", client.Organization)
	assert.Equal(t, "test-token", client.APIToken)
}

//nolint:funlen // table-driven test with multiple test cases
func TestGitHubClient_GetCatalog_User(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		linkHeader   string
		pagination   *PaginationParams
		wantErr      bool
		apiPackages  []string // Raw package names from API
		wantRepos    []string // Expected repository names with prefix
		wantMore     bool
		wantNextPage string
	}{
		{
			name:        "success without pagination",
			statusCode:  http.StatusOK,
			apiPackages: []string{"package1", "package2", "package3"},
			wantRepos:   []string{"testuser/package1", "testuser/package2", "testuser/package3"},
			wantMore:    false,
		},
		{
			name:         "success with pagination",
			statusCode:   http.StatusOK,
			linkHeader:   `<https://api.github.com/user/packages?package_type=container&page=2>; rel="next"`,
			pagination:   &PaginationParams{N: 10, Last: "1"},
			apiPackages:  []string{"package1"},
			wantRepos:    []string{"testuser/package1"},
			wantMore:     true,
			wantNextPage: "2",
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			wantErr:    true,
		},
		{
			name:        "empty response",
			statusCode:  http.StatusOK,
			apiPackages: []string{},
			wantRepos:   []string{},
			wantMore:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				assert.Equal(t, "/user/packages", r.URL.Path)
				assert.Equal(t, "container", r.URL.Query().Get("package_type"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				if tt.pagination != nil {
					assert.Equal(t, "10", r.URL.Query().Get("per_page"))
					assert.Equal(t, "1", r.URL.Query().Get("page"))
				}

				if tt.linkHeader != "" {
					w.Header().Set("Link", tt.linkHeader)
				}
				w.WriteHeader(tt.statusCode)

				if tt.statusCode == http.StatusOK {
					packages := make([]GitHubPackage, len(tt.apiPackages))
					for i, name := range tt.apiPackages {
						packages[i] = GitHubPackage{
							ID:          i + 1,
							Name:        name,
							PackageType: "container",
							Visibility:  "public",
						}
					}
					_ = json.NewEncoder(w).Encode(packages)
				} else {
					_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
				}
			}))
			defer server.Close()

			client := NewGitHubClient("testuser", "test-token")
			client.api = &mockPackagesAPI{
				serverURL: server.URL,
				client:    client.BaseClient,
			}

			resp, err := client.GetCatalog(context.Background(), tt.pagination)

			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, 1, callCount, "API should be called once")
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.wantRepos, resp.Repositories)
			assert.Equal(t, tt.wantMore, resp.HasMore)
			if tt.wantNextPage != "" {
				assert.Equal(t, tt.wantNextPage, resp.Last)
			}
			assert.Equal(t, 1, callCount, "API should be called once")
		})
	}
}

//nolint:funlen // table-driven test with multiple test cases
func TestGitHubClient_GetCatalog_Org(t *testing.T) {
	tests := []struct {
		name         string
		org          string
		statusCode   int
		linkHeader   string
		pagination   *PaginationParams
		wantErr      bool
		apiPackages  []string // Raw package names from API
		wantRepos    []string // Expected repository names with prefix
		wantMore     bool
		wantNextPage string
	}{
		{
			name:        "success without pagination",
			org:         "myorg",
			statusCode:  http.StatusOK,
			apiPackages: []string{"org-package1", "org-package2"},
			wantRepos:   []string{"myorg/org-package1", "myorg/org-package2"},
			wantMore:    false,
		},
		{
			name:         "success with pagination",
			org:          "testorg",
			statusCode:   http.StatusOK,
			linkHeader:   `<https://api.github.com/orgs/testorg/packages?package_type=container&page=3>; rel="next"`,
			pagination:   &PaginationParams{N: 25, Last: "2"},
			apiPackages:  []string{"package10"},
			wantRepos:    []string{"testorg/package10"},
			wantMore:     true,
			wantNextPage: "3",
		},
		{
			name:       "organization not found",
			org:        "nonexistent",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name:       "forbidden access",
			org:        "private-org",
			statusCode: http.StatusForbidden,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				assert.Contains(t, r.URL.Path, "/orgs/"+tt.org+"/packages")
				assert.Equal(t, "container", r.URL.Query().Get("package_type"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				if tt.pagination != nil {
					assert.Equal(t, "25", r.URL.Query().Get("per_page"))
					assert.Equal(t, "2", r.URL.Query().Get("page"))
				}

				if tt.linkHeader != "" {
					w.Header().Set("Link", tt.linkHeader)
				}
				w.WriteHeader(tt.statusCode)

				if tt.statusCode == http.StatusOK {
					packages := make([]GitHubPackage, len(tt.apiPackages))
					for i, name := range tt.apiPackages {
						packages[i] = GitHubPackage{
							ID:          i + 1,
							Name:        name,
							PackageType: "container",
							Visibility:  "public",
						}
					}
					_ = json.NewEncoder(w).Encode(packages)
				} else {
					_, _ = w.Write([]byte(`{"message":"error"}`))
				}
			}))
			defer server.Close()

			client := NewGitHubOrgClient(tt.org, "test-token")
			client.api = &mockPackagesAPI{
				serverURL: server.URL,
				client:    client.BaseClient,
			}

			resp, err := client.GetCatalog(context.Background(), tt.pagination)

			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, 1, callCount, "API should be called once")
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.wantRepos, resp.Repositories)
			assert.Equal(t, tt.wantMore, resp.HasMore)
			if tt.wantNextPage != "" {
				assert.Equal(t, tt.wantNextPage, resp.Last)
			}
			assert.Equal(t, 1, callCount, "API should be called once")
		})
	}
}

//nolint:funlen // table-driven test with many edge cases
func TestParseGitHubLinkHeader(t *testing.T) {
	tests := []struct {
		name         string
		linkHeader   string
		wantHasMore  bool
		wantNextPage string
		wantN        int
	}{
		{
			name:         "with next page",
			linkHeader:   `<https://api.github.com/user/packages?page=2>; rel="next", <https://api.github.com/user/packages?page=10>; rel="last"`,
			wantHasMore:  true,
			wantNextPage: "2",
			wantN:        0,
		},
		{
			name:         "with next page and per_page",
			linkHeader:   `<https://api.github.com/user/packages?package_type=container&per_page=1&page=2>; rel="next", <https://api.github.com/user/packages?package_type=container&per_page=1&page=6>; rel="last"`,
			wantHasMore:  true,
			wantNextPage: "2",
			wantN:        1,
		},
		{
			name:         "with next page and larger per_page",
			linkHeader:   `<https://api.github.com/user/packages?package_type=container&per_page=50&page=3>; rel="next", <https://api.github.com/user/packages?package_type=container&per_page=50&page=10>; rel="last"`,
			wantHasMore:  true,
			wantNextPage: "3",
			wantN:        50,
		},
		{
			name:         "full link header with next, prev, first, last",
			linkHeader:   `<https://api.github.com/user/packages?package_type=container&per_page=1&page=3>; rel="next", <https://api.github.com/user/packages?package_type=container&per_page=1&page=1>; rel="prev", <https://api.github.com/user/packages?package_type=container&per_page=1&page=1>; rel="first", <https://api.github.com/user/packages?package_type=container&per_page=1&page=6>; rel="last"`,
			wantHasMore:  true,
			wantNextPage: "3",
			wantN:        1,
		},
		{
			name:         "last page with prev and first (no next)",
			linkHeader:   `<https://api.github.com/user/packages?package_type=container&per_page=10&page=5>; rel="prev", <https://api.github.com/user/packages?package_type=container&per_page=10&page=1>; rel="first"`,
			wantHasMore:  false,
			wantNextPage: "",
			wantN:        0,
		},
		{
			name:         "last page (no next)",
			linkHeader:   `<https://api.github.com/user/packages?page=1>; rel="prev", <https://api.github.com/user/packages?page=1>; rel="first"`,
			wantHasMore:  false,
			wantNextPage: "",
			wantN:        0,
		},
		{
			name:         "empty header",
			linkHeader:   "",
			wantHasMore:  false,
			wantNextPage: "",
			wantN:        0,
		},
		{
			name:         "only next relation",
			linkHeader:   `<https://api.github.com/orgs/myorg/packages?package_type=container&page=3>; rel="next"`,
			wantHasMore:  true,
			wantNextPage: "3",
			wantN:        0,
		},
		{
			name:         "malformed link",
			linkHeader:   `malformed link header`,
			wantHasMore:  false,
			wantNextPage: "",
			wantN:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseGitHubLinkHeader(tt.linkHeader)
			assert.Equal(t, tt.wantHasMore, result.HasMore)
			assert.Equal(t, tt.wantNextPage, result.Last)
			assert.Equal(t, tt.wantN, result.N)
		})
	}
}

func TestGitHubClient_NetworkError(t *testing.T) {
	client := NewGitHubClient("testuser", "test-token")
	client.HTTPClient.Transport = &fakeRoundTripper{}

	_, err := client.GetCatalog(context.Background(), nil)
	require.Error(t, err)
}

func TestGitHubClient_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{invalid json}"))
	}))
	defer server.Close()

	client := NewGitHubClient("testuser", "test-token")
	client.api = &mockPackagesAPI{
		serverURL: server.URL,
		client:    client.BaseClient,
	}

	_, err := client.GetCatalog(context.Background(), nil)
	require.Error(t, err)
}

func TestGithubPackagesAPI_GetUserPackages_NetworkError(t *testing.T) {
	client := &BaseClient{HTTPClient: &http.Client{}, BaseURL: "http://example.com"}
	client.HTTPClient.Transport = &fakeRoundTripper{}

	api := &githubPackagesAPI{
		baseClient: client,
		apiToken:   "test-token",
		baseURL:    "http://example.com",
	}

	_, err := api.getUserPackages(context.Background(), nil)
	require.Error(t, err)
}

//nolint:funlen
func TestGithubPackagesAPI_GetUserPackages(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		linkHeader   string
		pagination   *PaginationParams
		wantErr      bool
		wantRepos    []string
		wantMore     bool
		wantNextPage string
		wantN        int
	}{
		{
			name:       "success without pagination",
			statusCode: http.StatusOK,
			wantRepos:  []string{"pkg1", "pkg2"},
			wantMore:   false,
		},
		{
			name:         "success with pagination and per_page",
			statusCode:   http.StatusOK,
			linkHeader:   `<https://api.github.com/user/packages?package_type=container&per_page=10&page=2>; rel="next"`,
			pagination:   &PaginationParams{N: 10, Last: "1"},
			wantRepos:    []string{"pkg1"},
			wantMore:     true,
			wantNextPage: "2",
			wantN:        10,
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			wantErr:    true,
		},
		{
			name:       "invalid json response",
			statusCode: http.StatusOK,
			wantRepos:  []string{},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/user/packages", r.URL.Path)
				assert.Equal(t, "container", r.URL.Query().Get("package_type"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				if tt.pagination != nil {
					assert.Equal(t, "10", r.URL.Query().Get("per_page"))
					assert.Equal(t, "1", r.URL.Query().Get("page"))
				}

				if tt.linkHeader != "" {
					w.Header().Set("Link", tt.linkHeader)
				}
				w.WriteHeader(tt.statusCode)

				switch {
				case tt.statusCode == http.StatusOK && !tt.wantErr:
					packages := make([]GitHubPackage, len(tt.wantRepos))
					for i, name := range tt.wantRepos {
						packages[i] = GitHubPackage{
							ID:          i + 1,
							Name:        name,
							PackageType: "container",
							Visibility:  "public",
						}
					}
					_ = json.NewEncoder(w).Encode(packages)
				case tt.statusCode == http.StatusOK && tt.wantErr:
					_, _ = w.Write([]byte("{invalid json}"))
				default:
					_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
				}
			}))
			defer server.Close()

			client := &BaseClient{HTTPClient: &http.Client{}, BaseURL: server.URL}
			api := &githubPackagesAPI{
				baseClient: client,
				apiToken:   "test-token",
				baseURL:    server.URL,
			}

			resp, err := api.getUserPackages(context.Background(), tt.pagination)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, len(tt.wantRepos), len(resp.Packages))
			for i, pkg := range resp.Packages {
				assert.Equal(t, tt.wantRepos[i], pkg.Name)
			}
			assert.Equal(t, tt.wantMore, resp.HasMore)
			if tt.wantNextPage != "" {
				assert.Equal(t, tt.wantNextPage, resp.Last)
			}
			if tt.wantN > 0 {
				assert.Equal(t, tt.wantN, resp.N)
			}
		})
	}
}

func TestGithubPackagesAPI_GetOrgPackages_NetworkError(t *testing.T) {
	client := &BaseClient{HTTPClient: &http.Client{}, BaseURL: "http://example.com"}
	client.HTTPClient.Transport = &fakeRoundTripper{}

	api := &githubPackagesAPI{
		baseClient: client,
		apiToken:   "test-token",
		baseURL:    "http://example.com",
	}

	_, err := api.getOrgPackages(context.Background(), "testorg", nil)
	require.Error(t, err)
}

//nolint:funlen
func TestGithubPackagesAPI_GetOrgPackages(t *testing.T) {
	tests := []struct {
		name         string
		org          string
		statusCode   int
		linkHeader   string
		pagination   *PaginationParams
		wantErr      bool
		wantRepos    []string
		wantMore     bool
		wantNextPage string
		wantN        int
	}{
		{
			name:       "success without pagination",
			org:        "testorg",
			statusCode: http.StatusOK,
			wantRepos:  []string{"org-pkg1", "org-pkg2"},
			wantMore:   false,
		},
		{
			name:         "success with pagination and per_page",
			org:          "myorg",
			statusCode:   http.StatusOK,
			linkHeader:   `<https://api.github.com/orgs/myorg/packages?package_type=container&per_page=25&page=3>; rel="next"`,
			pagination:   &PaginationParams{N: 25, Last: "2"},
			wantRepos:    []string{"pkg10"},
			wantMore:     true,
			wantNextPage: "3",
			wantN:        25,
		},
		{
			name:       "organization not found",
			org:        "nonexistent",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name:       "invalid json response",
			org:        "testorg",
			statusCode: http.StatusOK,
			wantRepos:  []string{},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "/orgs/"+tt.org+"/packages")
				assert.Equal(t, "container", r.URL.Query().Get("package_type"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				if tt.pagination != nil {
					assert.Equal(t, "25", r.URL.Query().Get("per_page"))
					assert.Equal(t, "2", r.URL.Query().Get("page"))
				}

				if tt.linkHeader != "" {
					w.Header().Set("Link", tt.linkHeader)
				}
				w.WriteHeader(tt.statusCode)

				switch {
				case tt.statusCode == http.StatusOK && !tt.wantErr:
					packages := make([]GitHubPackage, len(tt.wantRepos))
					for i, name := range tt.wantRepos {
						packages[i] = GitHubPackage{
							ID:          i + 1,
							Name:        name,
							PackageType: "container",
							Visibility:  "public",
						}
					}
					_ = json.NewEncoder(w).Encode(packages)
				case tt.statusCode == http.StatusOK && tt.wantErr:
					_, _ = w.Write([]byte("{invalid json}"))
				default:
					_, _ = w.Write([]byte(`{"message":"not found"}`))
				}
			}))
			defer server.Close()

			client := &BaseClient{HTTPClient: &http.Client{}, BaseURL: server.URL}
			api := &githubPackagesAPI{
				baseClient: client,
				apiToken:   "test-token",
				baseURL:    server.URL,
			}

			resp, err := api.getOrgPackages(context.Background(), tt.org, tt.pagination)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, len(tt.wantRepos), len(resp.Packages))
			for i, pkg := range resp.Packages {
				assert.Equal(t, tt.wantRepos[i], pkg.Name)
			}
			assert.Equal(t, tt.wantMore, resp.HasMore)
			if tt.wantNextPage != "" {
				assert.Equal(t, tt.wantNextPage, resp.Last)
			}
			if tt.wantN > 0 {
				assert.Equal(t, tt.wantN, resp.N)
			}
		})
	}
}

//nolint:funlen // table-driven test with test server
func TestGitHubClient_DeleteManifest_User(t *testing.T) {
	tests := []struct {
		name          string
		repository    string
		reference     string
		setupServer   func(w http.ResponseWriter, r *http.Request)
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:       "delete by tag success",
			repository: "user/my-app",
			reference:  "v1.0.0",
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/user/packages/container/my-app/versions":
					versions := []GitHubPackageVersion{
						{
							ID:   12345,
							Name: "sha256:abc123",
							Metadata: GitHubPackageMetadata{
								Container: GitHubContainerMetadata{
									Tags: []string{"v1.0.0", "latest"},
								},
							},
						},
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(versions)
				case r.Method == http.MethodDelete && r.URL.Path == "/user/packages/container/my-app/versions/12345":
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantErr: false,
		},
		{
			name:       "delete by digest success",
			repository: "user/my-app",
			reference:  "sha256:abc123",
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/user/packages/container/my-app/versions":
					versions := []GitHubPackageVersion{
						{
							ID:   12345,
							Name: "sha256:abc123",
							Metadata: GitHubPackageMetadata{
								Container: GitHubContainerMetadata{
									Tags: []string{"v1.0.0"},
								},
							},
						},
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(versions)
				case r.Method == http.MethodDelete && r.URL.Path == "/user/packages/container/my-app/versions/12345":
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantErr: false,
		},
		{
			name:       "version not found",
			repository: "user/my-app",
			reference:  "nonexistent",
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					versions := []GitHubPackageVersion{}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(versions)
				}
			},
			wantErr:       true,
			wantErrSubstr: "package version not found for reference",
		},
		{
			name:       "delete forbidden",
			repository: "user/my-app",
			reference:  "v1.0.0",
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					versions := []GitHubPackageVersion{
						{
							ID:   12345,
							Name: "sha256:abc123",
							Metadata: GitHubPackageMetadata{
								Container: GitHubContainerMetadata{
									Tags: []string{"v1.0.0"},
								},
							},
						},
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(versions)
				case http.MethodDelete:
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"message":"forbidden"}`))
				}
			},
			wantErr:       true,
			wantErrSubstr: "insufficient permissions",
		},
	}

	//nolint:dupl // Test patterns are similar but test different client types
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				tt.setupServer(w, r)
			}))
			defer server.Close()

			client := NewGitHubClient("testuser", "test-token")
			client.api = &githubPackagesAPI{
				baseClient: client.BaseClient,
				apiToken:   "test-token",
				baseURL:    server.URL,
			}

			err := client.DeleteManifest(context.Background(), tt.repository, tt.reference)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrSubstr != "" {
					assert.Contains(t, err.Error(), tt.wantErrSubstr)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

//nolint:funlen // table-driven test with test server
func TestGitHubClient_DeleteManifest_Org(t *testing.T) {
	tests := []struct {
		name          string
		repository    string
		reference     string
		setupServer   func(w http.ResponseWriter, r *http.Request)
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:       "delete by tag success",
			repository: "org/my-app",
			reference:  "latest",
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/orgs/myorg/packages/container/my-app/versions":
					versions := []GitHubPackageVersion{
						{
							ID:   67890,
							Name: "sha256:def456",
							Metadata: GitHubPackageMetadata{
								Container: GitHubContainerMetadata{
									Tags: []string{"latest"},
								},
							},
						},
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(versions)
				case r.Method == http.MethodDelete && r.URL.Path == "/orgs/myorg/packages/container/my-app/versions/67890":
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantErr: false,
		},
		{
			name:       "version not found",
			repository: "org/my-app",
			reference:  "v2.0.0",
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					versions := []GitHubPackageVersion{
						{
							ID:   67890,
							Name: "sha256:def456",
							Metadata: GitHubPackageMetadata{
								Container: GitHubContainerMetadata{
									Tags: []string{"v1.0.0"},
								},
							},
						},
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(versions)
				}
			},
			wantErr:       true,
			wantErrSubstr: "package version not found for reference",
		},
	}

	//nolint:dupl // Test patterns are similar but test different client types
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				tt.setupServer(w, r)
			}))
			defer server.Close()

			client := NewGitHubOrgClient("myorg", "test-token")
			client.api = &githubPackagesAPI{
				baseClient: client.BaseClient,
				apiToken:   "test-token",
				baseURL:    server.URL,
			}

			err := client.DeleteManifest(context.Background(), tt.repository, tt.reference)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrSubstr != "" {
					assert.Contains(t, err.Error(), tt.wantErrSubstr)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

//nolint:funlen
func TestGitHubClient_FindPackageVersionID_Pagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		w.WriteHeader(http.StatusOK)

		switch page {
		case "1", "":
			versions := make([]GitHubPackageVersion, 100)
			for i := range 100 {
				versions[i] = GitHubPackageVersion{
					ID:   i + 1,
					Name: fmt.Sprintf("sha256:hash%d", i),
					Metadata: GitHubPackageMetadata{
						Container: GitHubContainerMetadata{
							Tags: []string{fmt.Sprintf("v1.%d.0", i)},
						},
					},
				}
			}
			_ = json.NewEncoder(w).Encode(versions)
		case "2":
			versions := []GitHubPackageVersion{
				{
					ID:   201,
					Name: "sha256:target",
					Metadata: GitHubPackageMetadata{
						Container: GitHubContainerMetadata{
							Tags: []string{"target-tag"},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(versions)
		default:
			_ = json.NewEncoder(w).Encode([]GitHubPackageVersion{})
		}
	}))
	defer server.Close()

	client := NewGitHubClient("testuser", "test-token")
	client.api = &githubPackagesAPI{
		baseClient: client.BaseClient,
		apiToken:   "test-token",
		baseURL:    server.URL,
	}

	versionID, err := client.findPackageVersionID(context.Background(), "my-app", "target-tag")
	require.NoError(t, err)
	assert.Equal(t, 201, versionID)
}

func TestGitHubClient_ListPackageVersions_NetworkError(t *testing.T) {
	client := NewGitHubClient("testuser", "test-token")
	client.HTTPClient.Transport = &fakeRoundTripper{}

	_, err := client.listPackageVersions(context.Background(), "my-app", nil)
	require.Error(t, err)
}

func TestGitHubClient_FindPackageVersionID_NetworkError(t *testing.T) {
	client := NewGitHubClient("testuser", "test-token")
	client.HTTPClient.Transport = &fakeRoundTripper{}

	_, err := client.findPackageVersionID(context.Background(), "my-app", "v1.0.0")
	require.Error(t, err)
}

func TestGitHubClient_DeletePackageVersion_NetworkError(t *testing.T) {
	client := NewGitHubClient("testuser", "test-token")
	client.HTTPClient.Transport = &fakeRoundTripper{}

	err := client.deletePackageVersion(context.Background(), "my-app", 123)
	require.Error(t, err)
}

func TestGitHubClient_ListPackageVersions_StatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
	}))
	defer server.Close()

	client := NewGitHubClient("testuser", "test-token")
	client.api = &githubPackagesAPI{
		baseClient: client.BaseClient,
		apiToken:   "test-token",
		baseURL:    server.URL,
	}

	_, err := client.listPackageVersions(context.Background(), "my-app", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list package versions failed")
}

func TestGitHubClient_ListPackageVersions_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`invalid json`))
	}))
	defer server.Close()

	client := NewGitHubClient("testuser", "test-token")
	client.api = &githubPackagesAPI{
		baseClient: client.BaseClient,
		apiToken:   "test-token",
		baseURL:    server.URL,
	}

	_, err := client.listPackageVersions(context.Background(), "my-app", nil)
	require.Error(t, err)
}

func TestGitHubClient_DeletePackageVersion_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewGitHubClient("testuser", "test-token")
	client.api = &githubPackagesAPI{
		baseClient: client.BaseClient,
		apiToken:   "test-token",
		baseURL:    server.URL,
	}

	err := client.deletePackageVersion(context.Background(), "my-app", 123)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "package version not found")
}

func TestGitHubClient_DeletePackageVersion_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"internal error"}`))
	}))
	defer server.Close()

	client := NewGitHubClient("testuser", "test-token")
	client.api = &githubPackagesAPI{
		baseClient: client.BaseClient,
		apiToken:   "test-token",
		baseURL:    server.URL,
	}

	err := client.deletePackageVersion(context.Background(), "my-app", 123)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete package version failed")
}

//nolint:dupl // Test patterns are similar but test different client types and paths
func TestGitHubClient_DeleteManifest_MultiSegmentRepository_User(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Package name "textbee/api" is URL encoded to "textbee%2Fapi"
		// The server receives the escaped path in RawPath or can reconstruct from RequestURI
		requestPath := r.URL.Path
		if r.URL.RawPath != "" {
			requestPath = r.URL.RawPath
		}
		t.Logf("Request: %s %s", r.Method, requestPath)
		switch {
		case r.Method == http.MethodGet && requestPath == "/user/packages/container/textbee%2Fapi/versions":
			versions := []GitHubPackageVersion{
				{
					ID:   99999,
					Name: "sha256:multi123",
					Metadata: GitHubPackageMetadata{
						Container: GitHubContainerMetadata{
							Tags: []string{"v2.0.0"},
						},
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(versions)
		case r.Method == http.MethodDelete && requestPath == "/user/packages/container/textbee%2Fapi/versions/99999":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Logf("Unexpected request path: %s", requestPath)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubClient("testuser", "test-token")
	client.api = &githubPackagesAPI{
		baseClient: client.BaseClient,
		apiToken:   "test-token",
		baseURL:    server.URL,
	}

	err := client.DeleteManifest(context.Background(), "eznix86/textbee/api", "v2.0.0")
	require.NoError(t, err)
}

//nolint:dupl // Test patterns are similar but test different client types and paths
func TestGitHubClient_DeleteManifest_MultiSegmentRepository_Org(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Package name "mycompany/backend/service" is URL encoded to "mycompany%2Fbackend%2Fservice"
		// The server receives the escaped path in RawPath or can reconstruct from RequestURI
		requestPath := r.URL.Path
		if r.URL.RawPath != "" {
			requestPath = r.URL.RawPath
		}
		t.Logf("Request: %s %s", r.Method, requestPath)
		switch {
		case r.Method == http.MethodGet && requestPath == "/orgs/acme/packages/container/mycompany%2Fbackend%2Fservice/versions":
			versions := []GitHubPackageVersion{
				{
					ID:   88888,
					Name: "sha256:orgmulti",
					Metadata: GitHubPackageMetadata{
						Container: GitHubContainerMetadata{
							Tags: []string{"prod"},
						},
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(versions)
		case r.Method == http.MethodDelete && requestPath == "/orgs/acme/packages/container/mycompany%2Fbackend%2Fservice/versions/88888":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Logf("Unexpected request path: %s", requestPath)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubOrgClient("acme", "test-token")
	client.api = &githubPackagesAPI{
		baseClient: client.BaseClient,
		apiToken:   "test-token",
		baseURL:    server.URL,
	}

	err := client.DeleteManifest(context.Background(), "acme/mycompany/backend/service", "prod")
	require.NoError(t, err)
}

func TestGitHubClient_DeleteManifest_DisableDelete(t *testing.T) {
	// Track if delete endpoint was called
	deleteCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user/packages/container/my-app/versions":
			// Return a version list
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id": 12345, "name": "sha256:abc123", "metadata": {"container": {"tags": ["latest"]}}}]`))
		case r.Method == http.MethodDelete:
			deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubClient("testuser", "test-token")
	client.DisableDelete = true
	client.api = &githubPackagesAPI{
		baseClient: client.BaseClient,
		apiToken:   "test-token",
		baseURL:    server.URL,
	}

	// Should not actually call delete
	err := client.DeleteManifest(context.Background(), "testuser/my-app", "latest")
	require.NoError(t, err)
	assert.False(t, deleteCalled, "DELETE should not have been called when DisableDelete is true")
}
