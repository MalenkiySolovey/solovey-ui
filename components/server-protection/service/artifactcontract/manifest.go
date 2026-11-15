// Package artifactcontract provides the existing typed artifact evidence.
// Filesystem publication, verification and protected roots stay with the owner.
package artifactcontract

const ManifestVersion = 1

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type Manifest struct {
	Version     int    `json:"version"`
	OperationID string `json:"operationId"`
	Revision    string `json:"revision"`
	CreatedAt   int64  `json:"createdAt"`
	Files       []File `json:"files"`
}
