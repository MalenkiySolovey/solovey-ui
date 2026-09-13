// Package openwrt owns the fixed deployment profile inputs for the OpenWrt
// package. It does not detect a running OS or select an application runtime.
package openwrt

import (
	"errors"
	"fmt"
	"path"
	"strings"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

const (
	ProfileSchemaV1              = "solovey-ui/deploy-openwrt-profile/v1"
	DefaultDatabaseFolder        = "/etc/solovey-ui/db"
	DefaultOpenWrtInstanceIDPath = "/etc/solovey-ui/openwrt-instance-id"
	DefaultBrokerSocketRoot      = broker.StandaloneSocketRoot
	DefaultInstallRoot           = "/usr/lib/solovey-ui"
	DefaultReleaseRoot           = DefaultInstallRoot + "/releases"
	DefaultTemporaryRoot         = "/tmp/solovey-ui"
)

var (
	ErrVolatileDurableState     = errors.New("OpenWrt durable database state is under a volatile root")
	ErrUnprovenDurableState     = errors.New("OpenWrt durable database state is not proven persistent")
	ErrInsufficientDurableSpace = errors.New("OpenWrt durable database state has insufficient free space")
)

// ProfileV1 carries only deployment-owned path facts. Optional components own
// their runtime contracts and bind them at their component-local composition
// boundary; the deployment profile does not know concrete component names.
type ProfileV1 struct {
	Schema           string `json:"schema"`
	DatabaseFolder   string `json:"databaseFolder"`
	BrokerSocketRoot string `json:"brokerSocketRoot"`
	InstallRoot      string `json:"installRoot"`
	ReleaseRoot      string `json:"releaseRoot"`
	TemporaryRoot    string `json:"temporaryRoot"`
}

// DurableStateEvidence is derived by the deployment owner. It deliberately
// contains no filesystem discovery behavior in the portable profile itself.
type DurableStateEvidence struct {
	MountPath      string
	Persistent     bool
	AvailableBytes uint64
	RequiredBytes  uint64
}

func DefaultProfile() ProfileV1 {
	return ProfileV1{
		Schema: ProfileSchemaV1, DatabaseFolder: DefaultDatabaseFolder, BrokerSocketRoot: DefaultBrokerSocketRoot,
		InstallRoot: DefaultInstallRoot, ReleaseRoot: DefaultReleaseRoot, TemporaryRoot: DefaultTemporaryRoot,
	}
}

// DatabaseEnvironment exposes the sole existing application storage seam.
func (p ProfileV1) DatabaseEnvironment() (map[string]string, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return map[string]string{"SUI_DB_FOLDER": p.DatabaseFolder}, nil
}

func (p ProfileV1) Validate() error {
	if p.Schema != ProfileSchemaV1 || !canonicalAbsolute(p.DatabaseFolder) || isVolatileDurableRoot(p.DatabaseFolder) {
		if isVolatileDurableRoot(p.DatabaseFolder) {
			return fmt.Errorf("%w: %s", ErrVolatileDurableState, p.DatabaseFolder)
		}
		return errors.New("OpenWrt deployment profile database path is invalid")
	}
	if !canonicalAbsolute(p.BrokerSocketRoot) || !canonicalAbsolute(p.InstallRoot) || !canonicalAbsolute(p.ReleaseRoot) || !canonicalAbsolute(p.TemporaryRoot) ||
		p.ReleaseRoot != path.Join(p.InstallRoot, "releases") {
		return errors.New("OpenWrt deployment profile non-database paths are invalid")
	}
	for _, root := range []string{p.BrokerSocketRoot, p.InstallRoot, p.TemporaryRoot} {
		if pathOverlaps(p.DatabaseFolder, root) {
			return errors.New("OpenWrt deployment profile mixes durable database state with another path owner")
		}
	}
	if pathOverlaps(p.BrokerSocketRoot, p.TemporaryRoot) {
		return errors.New("OpenWrt deployment profile mixes runtime path owners")
	}
	return nil
}

// ValidateDurableState checks evidence prepared by the deployment owner once,
// before startup. A zero requirement is rejected so this profile never invents
// a hardware capacity threshold without deployment context.
func (p ProfileV1) ValidateDurableState(evidence DurableStateEvidence) error {
	if err := p.Validate(); err != nil {
		return err
	}
	canonicalMount := evidence.MountPath == "/" || canonicalAbsolute(evidence.MountPath)
	if !canonicalMount || !pathContains(evidence.MountPath, p.DatabaseFolder) || !evidence.Persistent {
		return fmt.Errorf("%w: database folder %s", ErrUnprovenDurableState, p.DatabaseFolder)
	}
	if evidence.RequiredBytes == 0 || evidence.AvailableBytes < evidence.RequiredBytes {
		return fmt.Errorf("%w: available=%d required=%d", ErrInsufficientDurableSpace, evidence.AvailableBytes, evidence.RequiredBytes)
	}
	return nil
}

func canonicalAbsolute(value string) bool {
	return value != "" && strings.HasPrefix(value, "/") && value != "/" && path.Clean(value) == value && !strings.ContainsAny(value, "\x00\r\n\t")
}

func isVolatileDurableRoot(value string) bool {
	return pathContains("/tmp", value) || pathContains("/var", value)
}

func pathContains(root, value string) bool {
	if root == "/" {
		return strings.HasPrefix(value, "/")
	}
	return value == root || strings.HasPrefix(value, root+"/")
}

func pathOverlaps(left, right string) bool {
	return pathContains(left, right) || pathContains(right, left)
}
