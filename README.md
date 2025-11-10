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

## Installation

```bash
go get github.com/eznix86/registry-client
```

## Usage

### Basic Setup

```go
import registryclient "github.com/eznix86/registry-client"

// Create a client with basic authentication
client := &registryclient.Client{
    BaseURL: "https://registry.example.com",
    Auth: registryclient.BasicAuth{
        Username: "user",
        Password: "pass",
    },
}

// Or with bearer token
client := &registryclient.Client{
    BaseURL: "https://registry.example.com",
    Auth: registryclient.BearerAuth{
        Token: "your-token",
    },
}
```

### Configuration Options

```go
client := &registryclient.Client{
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

### Client Methods

- `HealthCheck(ctx) (*int, error)` - Check registry availability
- `GetCatalog(ctx, pagination) (*CatalogResponse, error)` - List repositories
- `ListTags(ctx, repository, pagination) (*TagsResponse, error)` - List tags for a repository
- `GetManifest(ctx, repository, reference, acceptHeaders...) (*ManifestResponse, error)` - Get image manifest
- `HasManifest(ctx, repository, reference, acceptHeaders...) (bool, error)` - Check if manifest exists
- `GetBlob(ctx, repository, digest) (*BlobResponse, error)` - Get blob content
- `HasBlob(ctx, repository, digest) (bool, error)` - Check if blob exists
- `DeleteManifest(ctx, repository, digest, acceptHeaders...) error` - Delete manifest by digest

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
