package registryclient

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryClient_Interface(t *testing.T) {
	t.Run("Client implements RegistryClient", func(t *testing.T) {
		var _ RegistryClient = &BaseClient{HTTPClient: &http.Client{}}
	})

	t.Run("GitHubClient implements RegistryClient", func(t *testing.T) {
		var _ RegistryClient = &GitHubClient{}
	})
}

func TestRegistryClient_Polymorphism(t *testing.T) {
	tests := []struct {
		name   string
		client RegistryClient
	}{
		{
			name: "standard registry client",
			client: &BaseClient{
				HTTPClient: &http.Client{},
				BaseURL:    "https://registry-1.docker.io",
			},
		},
		{
			name:   "github registry client",
			client: NewGitHubClient("testuser", "test-token"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.client)

			ctx := context.Background()

			statusCode, err := tt.client.HealthCheck(ctx)
			require.NoError(t, err)
			require.NotZero(t, statusCode)
		})
	}
}
