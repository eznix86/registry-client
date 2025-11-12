[![Go Report Card](https://goreportcard.com/badge/github.com/eznix86/registry-client)](https://goreportcard.com/report/github.com/eznix86/registry-client) [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
![Coverage](https://img.shields.io/badge/Coverage-97.3%25-brightgreen)
[![Go Version](https://img.shields.io/github/go-mod/go-version/eznix86/registry-client?style=flat-square)](https://golang.org/dl/)
[![Release](https://img.shields.io/github/v/release/eznix86/registry-client?style=flat-square)](https://github.com/eznix86/registry-client/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/eznix86/registry-client.svg)](https://pkg.go.dev/github.com/eznix86/registry-client)


# Registry Client

A Go client library for interacting with OCI/Docker container registries.

Used by [Docker Registry UI](https://github.com/eznix86/docker-registry-ui)

## Features

- Full support for Docker Distribution API v2
- OCI and Docker manifest handling
- Built-in retry logic with exponential backoff
- Authentication support (Basic Auth and Bearer Token)
- Pagination support for large result sets
- Optional logging interface
- Health check endpoint
- GitHub Container Registry support (user and organization packages)
- Safe delete operations with `DisableDelete` flag for testing

## Installation

```bash
go get github.com/eznix86/registry-client
```

## Usage

### Basic Setup

```go
import (
    "net/http"
    registryclient "github.com/eznix86/registry-client"
)

// Create a client with basic authentication
client := &registryclient.BaseClient{
    HTTPClient: &http.Client{},
    BaseURL:    "https://registry.example.com",
    Auth: registryclient.BasicAuth{
        Username: "user",
        Password: "pass",
    },
}

// Or with bearer token
client := &registryclient.BaseClient{
    HTTPClient: &http.Client{},
    BaseURL:    "https://registry.example.com",
    Auth: registryclient.BearerAuth{
        Token: "your-token",
    },
}
```

### Configuration Options

```go
client := &registryclient.BaseClient{
    HTTPClient:   &http.Client{},
    BaseURL:      "https://registry.example.com",
    Auth:         auth,
    RetryBackoff: 200 * time.Millisecond,  // Initial backoff duration
    MaxAttempts:  3,                        // Maximum retry attempts
    Logger:       logger,                   // Optional logger implementation
}
```

### Health Check

```go
status, err := client.HealthCheck(context.Background())
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Registry status: %d\n", *status)
```

### List Repositories

```go
catalog, err := client.GetCatalog(context.Background(), nil)
if err != nil {
    log.Fatal(err)
}

for _, repo := range catalog.Repositories {
    fmt.Println(repo)
}
```

### List Tags

```go
tags, err := client.ListTags(context.Background(), "my-repo", nil)
if err != nil {
    log.Fatal(err)
}

for _, tag := range tags.Tags {
    fmt.Println(tag)
}
```

### Get Manifest

```go
manifest, err := client.GetManifest(context.Background(), "my-repo", "latest")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Digest: %s\n", manifest.Digest)
fmt.Printf("Media Type: %s\n", manifest.MediaType)
```

### Get Blob (Image Config)

```go
blob, err := client.GetBlob(context.Background(), "my-repo", "sha256:abc123...")
if err != nil {
    log.Fatal(err)
}

config, err := registryclient.ParseConfigBlob(blob.Content)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Architecture: %s\n", config.Architecture)
fmt.Printf("OS: %s\n", config.OS)
```

### Pagination

```go
// Get first page with 50 items
pagination := &registryclient.PaginationParams{
    N: 50,
}

catalog, err := client.GetCatalog(context.Background(), pagination)
if err != nil {
    log.Fatal(err)
}

// Get next page
if catalog.HasMore {
    pagination.Last = catalog.Last
    next, err := client.GetCatalog(context.Background(), pagination)
    // ...
}
```

### Check Existence

```go
// Check if manifest exists
exists, err := client.HasManifest(context.Background(), "my-repo", "latest")
if err != nil {
    log.Fatal(err)
}

// Check if blob exists
exists, err = client.HasBlob(context.Background(), "my-repo", "sha256:abc123...")
if err != nil {
    log.Fatal(err)
}
```

### Delete Manifest

```go
// Note: reference must be a digest, not a tag
err := client.DeleteManifest(context.Background(), "my-repo", "sha256:abc123...")
if err != nil {
    log.Fatal(err)
}
```

#### Safe Delete Testing

Use `DisableDelete` flag to test delete operations without actually deleting resources:

```go
client := &registryclient.BaseClient{
    HTTPClient:    &http.Client{},
    BaseURL:       "https://registry.example.com",
    DisableDelete: true, // Prevents actual deletion, only logs
}

// This will only log the delete operation, not execute it
err := client.DeleteManifest(context.Background(), "my-repo", "sha256:abc123...")
// No error, but nothing was deleted
```

### GitHub Container Registry

For GitHub Container Registry (ghcr.io), use `GitHubClient`:

```go
import registryclient "github.com/eznix86/registry-client"

// For user packages
// Note: Pass your GitHub Personal Access Token directly (plain text)
// It will be properly encoded for ghcr.io registry access and used plain for API calls
client := registryclient.NewGitHubClient("username", "ghp_yourtoken")

// List user's container packages
catalog, err := client.GetCatalog(context.Background(), nil)
if err != nil {
    log.Fatal(err)
}

for _, pkg := range catalog.Repositories {
    fmt.Println(pkg)
}

// For organization packages
orgClient := registryclient.NewGitHubOrgClient("myorg", "ghp_yourtoken")
orgCatalog, err := orgClient.GetCatalog(context.Background(), nil)
if err != nil {
    log.Fatal(err)
}
```

### Custom Logger

Implement the `Logger` interface to add logging:

```go
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
}
```

## API Reference

### BaseClient Methods

- `HealthCheck(ctx) (int, error)` - Check registry availability
- `GetCatalog(ctx, pagination) (*CatalogResponse, error)` - List repositories
- `ListTags(ctx, repository, pagination) (*TagsResponse, error)` - List tags for a repository
- `GetManifest(ctx, repository, reference, acceptHeaders...) (*ManifestResponse, error)` - Get image manifest
- `HasManifest(ctx, repository, reference, acceptHeaders...) (bool, error)` - Check if manifest exists
- `GetBlob(ctx, repository, digest) (*BlobResponse, error)` - Get blob content
- `HasBlob(ctx, repository, digest) (bool, error)` - Check if blob exists
- `DeleteManifest(ctx, repository, digest, acceptHeaders...) error` - Delete manifest by digest

### GitHubClient Methods

GitHubClient embeds BaseClient and provides the same methods, with special handling for:
- `GetCatalog(ctx, pagination)` - Lists user or organization packages from GitHub API
- `DeleteManifest(ctx, repository, reference)` - Deletes package versions (works with tags or digests)

### Authentication

- `BasicAuth{Username, Password}` - HTTP Basic Authentication
- `BearerAuth{Token}` - HTTP Bearer Token Authentication

## Contributing

Contributions are welcome! Please follow these guidelines:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Run tests and ensure they pass
5. Commit your changes (`git commit -m 'Add amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

### Development Guidelines

- Write clear, idiomatic Go code
- Add tests for new functionality
- Update documentation as needed
- Follow existing code style and conventions
- Ensure all tests pass before submitting PR

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Support

For issues and feature requests, please use the [GitHub issue tracker](https://github.com/eznix86/registry-client/issues).
