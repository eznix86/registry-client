package registryclient

import "encoding/json"

// Manifest represents an OCI/Docker manifest with schema version and media type
type Manifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType"`
	Raw           json.RawMessage `json:"-"`
	ManifestData  any             `json:"-"`
}

// ImageConfig represents the configuration reference in a manifest
type ImageConfig struct {
	Digest string `json:"digest"`
}

// Layer represents a single layer in an image manifest
type Layer struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// ImageManifest represents an OCI/Docker image manifest
type ImageManifest struct {
	Config ImageConfig `json:"config"`
	Layers []Layer     `json:"layers"`
}

// Platform represents the platform information for a manifest
type Platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
}

// ManifestReference represents a reference to a platform-specific manifest
type ManifestReference struct {
	MediaType string   `json:"mediaType"`
	Digest    string   `json:"digest"`
	Platform  Platform `json:"platform"`
}

// ManifestList represents an OCI image index or Docker manifest list
type ManifestList struct {
	Manifests []ManifestReference `json:"manifests"`
}

// ContainerConfig represents the runtime configuration of a container
type ContainerConfig struct {
	User         string              `json:"User,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	Env          []string            `json:"Env,omitempty"`
	Entrypoint   []string            `json:"Entrypoint,omitempty"`
	Cmd          []string            `json:"Cmd,omitempty"`
	Volumes      map[string]struct{} `json:"Volumes,omitempty"`
	WorkingDir   string              `json:"WorkingDir,omitempty"`
	Labels       map[string]string   `json:"Labels,omitempty"`
	StopSignal   string              `json:"StopSignal,omitempty"`
}

// HistoryEntry represents a single layer in the image build history
type HistoryEntry struct {
	Created    string `json:"created"`
	CreatedBy  string `json:"created_by"`
	Comment    string `json:"comment,omitempty"`
	EmptyLayer bool   `json:"empty_layer,omitempty"`
}

// RootFS represents the root filesystem configuration
type RootFS struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

// ConfigBlob represents the OCI/Docker image configuration
type ConfigBlob struct {
	Architecture string          `json:"architecture"`
	OS           string          `json:"os"`
	Config       ContainerConfig `json:"config"`
	Created      string          `json:"created"`
	History      []HistoryEntry  `json:"history"`
	Rootfs       RootFS          `json:"rootfs"`
}
