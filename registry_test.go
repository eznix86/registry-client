package registryclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConfigBlob(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		wantErr   bool
		wantArch  string
		wantOS    string
		wantEmpty bool
	}{
		{name: "valid config blob", file: "testdata/config/valid-config.json", wantArch: "amd64", wantOS: "linux"},
		{name: "invalid JSON", file: "testdata/invalid/invalid-json.json", wantErr: true},
		{name: "empty content", file: "", wantEmpty: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var content []byte
			var err error

			if tt.file != "" {
				content, err = os.ReadFile(tt.file)
				require.NoError(t, err)
			} else {
				content = []byte(`{}`)
			}

			cfg, err := ParseConfigBlob(content)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, cfg)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)

			if !tt.wantEmpty {
				assert.Equal(t, tt.wantArch, cfg.Architecture)
				assert.Equal(t, tt.wantOS, cfg.OS)
			}
		})
	}
}

func TestParseManifest(t *testing.T) {
	manifestFiles, err := filepath.Glob("testdata/manifests/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, manifestFiles, "No manifest files found in testdata/manifests")

	for _, file := range manifestFiles {
		filename := filepath.Base(file)
		t.Run(filename, func(t *testing.T) {
			manifestJSON, err := os.ReadFile(filepath.Clean(file))
			require.NoError(t, err)

			m, err := ParseManifest(manifestJSON)

			// Special case: unsupported media type
			if filename == "unsupported-mediatype.json" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported mediaType")
				assert.Nil(t, m)
				return
			}

			// All other manifests should parse successfully
			require.NoError(t, err)
			require.NotNil(t, m)
			assert.Equal(t, 2, m.SchemaVersion)
			assert.Equal(t, manifestJSON, []byte(m.Raw))
		})
	}

	t.Run("invalid JSON", func(t *testing.T) {
		manifestJSON, err := os.ReadFile("testdata/invalid/invalid-json.json")
		require.NoError(t, err)

		m, err := ParseManifest(manifestJSON)
		require.Error(t, err)
		assert.Nil(t, m)
	})
}

func TestParseLinkHeader(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMore  bool
		wantLast  string
		wantN     int
	}{
		{name: "valid", input: `</v2/_catalog?last=myrepo&n=100>; rel="next"`, wantMore: true, wantLast: "myrepo", wantN: 100},
		{name: "without n", input: `</v2/_catalog?last=repo123>; rel="next"`, wantMore: true, wantLast: "repo123", wantN: 0},
		{name: "empty", input: "", wantMore: false, wantLast: "", wantN: 0},
		{name: "malformed", input: "invalid", wantMore: true, wantLast: ""},
		{name: "invalid URL", input: `<://invalid-url>; rel="next"`, wantMore: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLinkHeader(tt.input)
			assert.Equal(t, tt.wantMore, got.HasMore)
			assert.Equal(t, tt.wantLast, got.Last)
			if tt.name != "malformed" && tt.name != "invalid URL" {
				assert.Equal(t, tt.wantN, got.N)
			}
		})
	}
}

func TestApplyPagination(t *testing.T) {
	tests := []struct {
		name       string
		pagination *PaginationParams
		wantN      string
		wantLast   string
	}{
		{name: "with both", pagination: &PaginationParams{N: 50, Last: "lastrepo"}, wantN: "50", wantLast: "lastrepo"},
		{name: "only N", pagination: &PaginationParams{N: 25}, wantN: "25", wantLast: ""},
		{name: "only Last", pagination: &PaginationParams{Last: "somerepo"}, wantN: "", wantLast: "somerepo"},
		{name: "nil", pagination: nil, wantN: "", wantLast: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://example.com/v2/_catalog", nil)
			applyPagination(req, tt.pagination)

			query := req.URL.Query()
			assert.Equal(t, tt.wantN, query.Get("n"))
			assert.Equal(t, tt.wantLast, query.Get("last"))
		})
	}
}

func TestHealthCheck(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantCode   int
	}{
		{name: "healthy", statusCode: http.StatusOK, wantCode: http.StatusOK},
		{name: "unhealthy", statusCode: http.StatusUnauthorized, wantCode: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := &Client{BaseURL: server.URL}
			statusCode, err := client.HealthCheck(context.Background())

			require.NoError(t, err)
			require.NotNil(t, statusCode)
			assert.Equal(t, tt.wantCode, *statusCode)
		})
	}
}

func TestGetCatalog(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		linkHeader string
		pagination *PaginationParams
		wantErr    bool
		wantRepos  int
		wantMore   bool
	}{
		{name: "success", statusCode: http.StatusOK, wantRepos: 3, wantMore: false},
		{name: "with pagination", statusCode: http.StatusOK, linkHeader: `</v2/_catalog?last=repo10&n=10>; rel="next"`, pagination: &PaginationParams{N: 10, Last: "repo5"}, wantRepos: 3, wantMore: true},
		{name: "error", statusCode: http.StatusUnauthorized, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.linkHeader != "" {
					w.Header().Set("Link", tt.linkHeader)
				}
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"repositories": []string{"repo1", "repo2", "repo3"},
					})
				} else {
					_, _ = w.Write([]byte("unauthorized"))
				}
			}))
			defer server.Close()

			client := &Client{BaseURL: server.URL}
			resp, err := client.GetCatalog(context.Background(), tt.pagination)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Len(t, resp.Repositories, tt.wantRepos)
			assert.Equal(t, tt.wantMore, resp.HasMore)
		})
	}
}

func TestGetManifest(t *testing.T) {
	manifestJSON := `{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "config": {"digest": "sha256:config123"}, "layers": [{"digest": "sha256:layer1", "size": 1024}]}`

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
		wantDigest string
	}{
		{name: "success", statusCode: http.StatusOK, body: manifestJSON, wantDigest: "sha256:manifestdigest"},
		{name: "not found", statusCode: http.StatusNotFound, body: "manifest not found", wantErr: true},
		{name: "invalid JSON", statusCode: http.StatusOK, body: "{invalid json}", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.statusCode == http.StatusOK && tt.body == manifestJSON {
					w.Header().Set("Docker-Content-Digest", tt.wantDigest)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := &Client{BaseURL: server.URL}
			resp, err := client.GetManifest(context.Background(), "myrepo", "v1.0")

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.wantDigest, resp.Digest)
		})
	}
}

// testResourceExists is a helper to test HEAD request endpoints (HasManifest, HasBlob)
func testResourceExists(
	t *testing.T,
	expectedPath string,
	checkFunc func(*Client, context.Context, string, string) (bool, error),
) {
	t.Helper()

	tests := []struct {
		name       string
		statusCode int
		wantExists bool
		wantErr    bool
		maxAttempts int
	}{
		{name: "exists", statusCode: http.StatusOK, wantExists: true},
		{name: "not exist", statusCode: http.StatusNotFound, wantExists: false},
		{name: "unexpected", statusCode: http.StatusInternalServerError, wantErr: true, maxAttempts: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := &Client{BaseURL: server.URL, MaxAttempts: tt.maxAttempts}
			exists, err := checkFunc(client, context.Background(), "myrepo", "v1.0")

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantExists, exists)
		})
	}
}

func TestHasManifest(t *testing.T) {
	testResourceExists(t, "/v2/myrepo/manifests/v1.0", func(c *Client, ctx context.Context, repo, ref string) (bool, error) {
		return c.HasManifest(ctx, repo, ref)
	})
}

func TestGetBlob(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "success", statusCode: http.StatusOK},
		{name: "not found", statusCode: http.StatusNotFound, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blobContent := []byte("blob content data")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Docker-Content-Digest", "sha256:blobdigest")
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					_, _ = w.Write(blobContent)
				} else {
					_, _ = w.Write([]byte("blob not found"))
				}
			}))
			defer server.Close()

			client := &Client{BaseURL: server.URL}
			resp, err := client.GetBlob(context.Background(), "myrepo", "sha256:blobdigest")

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, blobContent, resp.Content)
		})
	}
}

func TestListTags(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		linkHeader string
		pagination *PaginationParams
		wantErr    bool
		wantTags   int
		wantMore   bool
	}{
		{name: "success", statusCode: http.StatusOK, wantTags: 3, wantMore: false},
		{name: "with pagination", statusCode: http.StatusOK, linkHeader: `</v2/myrepo/tags/list?last=v2.0&n=5>; rel="next"`, pagination: &PaginationParams{N: 5, Last: "v1.5"}, wantTags: 3, wantMore: true},
		{name: "not found", statusCode: http.StatusNotFound, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.linkHeader != "" {
					w.Header().Set("Link", tt.linkHeader)
				}
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"name": "myrepo",
						"tags": []string{"v1.0", "v1.1", "latest"},
					})
				} else {
					_, _ = w.Write([]byte("repository not found"))
				}
			}))
			defer server.Close()

			client := &Client{BaseURL: server.URL}
			resp, err := client.ListTags(context.Background(), "myrepo", tt.pagination)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Len(t, resp.Tags, tt.wantTags)
			assert.Equal(t, tt.wantMore, resp.HasMore)
		})
	}
}

func TestDeleteManifest(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "accepted", statusCode: http.StatusAccepted},
		{name: "no content", statusCode: http.StatusNoContent},
		{name: "not found", statusCode: http.StatusNotFound, wantErr: true},
		{name: "not allowed", statusCode: http.StatusMethodNotAllowed, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				if tt.wantErr {
					_, _ = w.Write([]byte("error"))
				}
			}))
			defer server.Close()

			client := &Client{BaseURL: server.URL}
			err := client.DeleteManifest(context.Background(), "myrepo", "sha256:digest")

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestHasBlob(t *testing.T) {
	testResourceExists(t, "/v2/myrepo/blobs/v1.0", func(c *Client, ctx context.Context, repo, ref string) (bool, error) {
		return c.HasBlob(ctx, repo, ref)
	})
}

func TestAddAcceptHeaders(t *testing.T) {
	tests := []struct {
		name       string
		custom     []string
		wantCount  int
		wantCustom bool
	}{
		{name: "default", wantCount: 4},
		{name: "custom", custom: []string{"application/custom+json"}, wantCount: 1, wantCustom: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
			addAcceptHeaders(req, tt.custom)

			accepts := req.Header["Accept"]
			assert.Len(t, accepts, tt.wantCount)

			if tt.wantCustom {
				assert.Equal(t, "application/custom+json", accepts[0])
			} else {
				assert.Contains(t, accepts, "application/vnd.oci.image.manifest.v1+json")
			}
		})
	}
}

// Error case tests for uncovered paths

func TestParseManifest_MalformedImageManifest(t *testing.T) {
	// Malformed OCI image manifest - invalid JSON structure for ImageManifest
	manifestJSON := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": "not an object"
	}`)

	m, err := ParseManifest(manifestJSON)
	require.Error(t, err)
	assert.Nil(t, m)
}

func TestParseManifest_MalformedDockerManifest(t *testing.T) {
	// Malformed Docker v2 manifest - invalid JSON structure
	manifestJSON := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
		"config": ["invalid"]
	}`)

	m, err := ParseManifest(manifestJSON)
	require.Error(t, err)
	assert.Nil(t, m)
}

func TestParseManifest_MalformedManifestList(t *testing.T) {
	// Malformed OCI image index/manifest list - invalid JSON structure
	manifestJSON := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.index.v1+json",
		"manifests": "not an array"
	}`)

	m, err := ParseManifest(manifestJSON)
	require.Error(t, err)
	assert.Nil(t, m)
}


// fakeRoundTripper simulates network errors
type fakeRoundTripper struct {
	err error
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return nil, errors.New("network error")
}

// errorReader simulates a reader that fails mid-stream
type errorReader struct {
	data      []byte
	readCount int
	failAfter int // fail after reading this many bytes
}

func (e *errorReader) Read(p []byte) (n int, err error) {
	if e.readCount >= e.failAfter {
		return 0, errors.New("connection reset by peer")
	}

	remaining := len(e.data) - e.readCount
	if remaining == 0 {
		return 0, io.EOF
	}

	n = len(p)
	if n > remaining {
		n = remaining
	}

	copy(p, e.data[e.readCount:e.readCount+n])
	e.readCount += n

	// Check if we should fail after this read
	if e.readCount >= e.failAfter {
		return n, errors.New("connection reset by peer")
	}

	return n, nil
}

func (e *errorReader) Close() error {
	return nil
}

// errorRoundTripper returns a response that fails mid-read
type errorRoundTripper struct {
	statusCode int
	headers    map[string]string
	body       []byte
	failAfter  int
}

func (e *errorRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp := &http.Response{
		StatusCode: e.statusCode,
		Header:     make(http.Header),
		Body:       &errorReader{data: e.body, failAfter: e.failAfter},
	}

	for k, v := range e.headers {
		resp.Header.Set(k, v)
	}

	return resp, nil
}

func TestHealthCheck_InvalidBaseURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid-url"}
	statusCode, err := client.HealthCheck(context.Background())

	require.Error(t, err)
	assert.Nil(t, statusCode)
}

func TestHealthCheck_NetworkError(t *testing.T) {
	client := &Client{
		BaseURL: "http://example.com",
	}
	client.Transport = &fakeRoundTripper{}
	statusCode, err := client.HealthCheck(context.Background())

	require.Error(t, err)
	assert.Nil(t, statusCode)
}

func TestGetCatalog_InvalidBaseURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid-url"}
	resp, err := client.GetCatalog(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetCatalog_NetworkError(t *testing.T) {
	client := &Client{
		BaseURL: "http://example.com",
	}
	client.Transport = &fakeRoundTripper{}
	resp, err := client.GetCatalog(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetCatalog_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{invalid json}"))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL}
	resp, err := client.GetCatalog(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetManifest_InvalidBaseURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid-url"}
	resp, err := client.GetManifest(context.Background(), "repo", "tag")

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetManifest_NetworkError(t *testing.T) {
	client := &Client{
		BaseURL: "http://example.com",
	}
	client.Transport = &fakeRoundTripper{}
	resp, err := client.GetManifest(context.Background(), "repo", "tag")

	require.Error(t, err)
	assert.Nil(t, resp)
}


func TestGetManifest_ReadBodyError(t *testing.T) {
	// Simulate connection cut mid-stream using custom RoundTripper
	client := &Client{BaseURL: "http://example.com"}
	client.Transport = &errorRoundTripper{
		statusCode: http.StatusOK,
		headers: map[string]string{
			"Docker-Content-Digest": "sha256:test",
		},
		body:      []byte(`{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json"}`),
		failAfter: 10, // Fail after reading 10 bytes
	}

	resp, err := client.GetManifest(context.Background(), "repo", "tag")

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "connection reset by peer")
}

func TestHasManifest_InvalidBaseURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid-url"}
	exists, err := client.HasManifest(context.Background(), "repo", "tag")

	require.Error(t, err)
	assert.False(t, exists)
}

func TestHasManifest_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // 418 - Unexpected status
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, MaxAttempts: 1}
	exists, err := client.HasManifest(context.Background(), "repo", "tag")

	require.Error(t, err)
	assert.False(t, exists)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestGetBlob_InvalidBaseURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid-url"}
	resp, err := client.GetBlob(context.Background(), "repo", "digest")

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetBlob_NetworkError(t *testing.T) {
	client := &Client{
		BaseURL: "http://example.com",
	}
	client.Transport = &fakeRoundTripper{}
	resp, err := client.GetBlob(context.Background(), "repo", "digest")

	require.Error(t, err)
	assert.Nil(t, resp)
}


func TestGetBlob_ReadBodyError(t *testing.T) {
	// Simulate connection cut mid-stream
	client := &Client{BaseURL: "http://example.com"}
	client.Transport = &errorRoundTripper{
		statusCode: http.StatusOK,
		headers: map[string]string{
			"Docker-Content-Digest": "sha256:blobdigest",
		},
		body:      []byte("this is blob content that will fail mid-read"),
		failAfter: 15, // Fail after reading 15 bytes
	}

	resp, err := client.GetBlob(context.Background(), "repo", "sha256:blobdigest")

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "connection reset by peer")
}

func TestListTags_InvalidBaseURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid-url"}
	resp, err := client.ListTags(context.Background(), "repo", nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestListTags_NetworkError(t *testing.T) {
	client := &Client{
		BaseURL: "http://example.com",
	}
	client.Transport = &fakeRoundTripper{}
	resp, err := client.ListTags(context.Background(), "repo", nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestListTags_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{invalid json}"))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL}
	resp, err := client.ListTags(context.Background(), "repo", nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestDeleteManifest_InvalidBaseURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid-url"}
	err := client.DeleteManifest(context.Background(), "repo", "digest")

	require.Error(t, err)
}

func TestDeleteManifest_NetworkError(t *testing.T) {
	client := &Client{
		BaseURL: "http://example.com",
	}
	client.Transport = &fakeRoundTripper{}
	err := client.DeleteManifest(context.Background(), "repo", "digest")

	require.Error(t, err)
}

func TestHasBlob_InvalidBaseURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid-url"}
	exists, err := client.HasBlob(context.Background(), "repo", "digest")

	require.Error(t, err)
	assert.False(t, exists)
}

func TestHasBlob_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // 418 - Unexpected status
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, MaxAttempts: 1}
	exists, err := client.HasBlob(context.Background(), "repo", "digest")

	require.Error(t, err)
	assert.False(t, exists)
	assert.Contains(t, err.Error(), "unexpected status")
}
