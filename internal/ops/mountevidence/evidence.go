// Package mountevidence provides bounded, policy-free Linux mount facts.
// Deployment and storage owners attach durability or volatility meaning; this
// package only parses and seals the current kernel mount identity.
package mountevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strconv"
	"strings"
)

const (
	SchemaV1                  = "solovey-ui/kernel-mount-evidence/v1"
	OverlayLabelSchemaV1      = "solovey-ui/overlay-label-evidence/v1"
	BackingProbeSchemaV1      = "solovey-ui/backing-probe-evidence/v1"
	OverlayLabelVisible       = "VISIBLE"
	OverlayLabelOpaque        = "OPAQUE"
	OverlayLabelInvalid       = "INVALID"
	BackingMappingPhysical    = "PHYSICAL_EXTENT"
	BackingMappingUnsupported = "UNSUPPORTED"
	MaxMounts                 = 4096
	MaxMountLine              = 64 << 10
	MaxMountInfo              = 2 << 20
	MaxOptionCount            = 256
	BackingProbeBytes         = 4096
	MaxBackingProbeExtents    = 16
)

var ErrUnavailable = errors.New("kernel mount evidence is unavailable")

// Fact is one sealed /proc/self/mountinfo record plus the read-only statfs
// proposition for the resolved target. Target is the caller's logical path;
// ResolvedTarget is the canonical filesystem path used for mountinfo/statfs.
// It intentionally contains no product policy such as "durable" or
// "volatile".
type Fact struct {
	Schema          string   `json:"schema"`
	Target          string   `json:"target"`
	ResolvedTarget  string   `json:"resolvedTarget"`
	MountID         uint64   `json:"mountId"`
	ParentID        uint64   `json:"parentId"`
	Device          string   `json:"device"`
	Root            string   `json:"root"`
	MountPoint      string   `json:"mountPoint"`
	Filesystem      string   `json:"filesystem"`
	Source          string   `json:"source"`
	MountOptions    []string `json:"mountOptions"`
	SuperOptions    []string `json:"superOptions"`
	MountReadOnly   bool     `json:"mountReadOnly"`
	KernelReadOnly  bool     `json:"kernelReadOnly"`
	FilesystemMagic int64    `json:"filesystemMagic"`
	Revision        string   `json:"revision"`
}

// OverlayLabelFact records whether one mount-option label can currently be
// resolved. The label is not treated as authority for the live backing object.
type OverlayLabelFact struct {
	Schema     string `json:"schema"`
	Label      string `json:"label"`
	Visibility string `json:"visibility"`
	Mount      Fact   `json:"mount,omitempty"`
	Revision   string `json:"revision"`
}

// BackingProbeFact records kernel I/O facts for a freshly created regular file
// seen through an OverlayFS mount. It intentionally does not assign durability
// policy to those facts.
type BackingProbeFact struct {
	Schema           string `json:"schema"`
	Target           string `json:"target"`
	MountRevision    string `json:"mountRevision"`
	Mapping          string `json:"mapping"`
	BytesWritten     uint64 `json:"bytesWritten"`
	ExtentCount      uint32 `json:"extentCount"`
	PhysicalBytes    uint64 `json:"physicalBytes"`
	FileSynced       bool   `json:"fileSynced"`
	FilesystemSynced bool   `json:"filesystemSynced"`
	DirectorySynced  bool   `json:"directorySynced"`
	Revision         string `json:"revision"`
}

func (f Fact) Writable() bool { return !f.MountReadOnly && !f.KernelReadOnly }

func (f Fact) OptionValue(name string) string {
	for _, option := range f.SuperOptions {
		key, value, ok := strings.Cut(option, "=")
		if ok && key == name {
			return value
		}
	}
	return ""
}

func (f Fact) HasOption(name string) bool {
	for _, option := range f.SuperOptions {
		if option == name {
			return true
		}
	}
	return false
}

func (f Fact) Validate() error {
	if f.Schema != SchemaV1 || !canonicalAbsolute(f.Target) || !canonicalAbsolute(f.ResolvedTarget) ||
		f.MountID == 0 || !validDevice(f.Device) || !canonicalAbsoluteAllowRoot(f.Root) ||
		!canonicalAbsoluteAllowRoot(f.MountPoint) || f.Filesystem == "" || len(f.Filesystem) > 64 ||
		f.Source == "" || len(f.Source) > 1024 || len(f.MountOptions) > MaxOptionCount || len(f.SuperOptions) > MaxOptionCount ||
		f.MountReadOnly != contains(f.MountOptions, "ro") || f.Revision != f.revision() {
		return errors.New("kernel mount evidence is malformed")
	}
	if !pathContains(f.MountPoint, f.ResolvedTarget) || !sortedUniqueSafe(f.MountOptions) || !sortedUniqueSafe(f.SuperOptions) {
		return errors.New("kernel mount evidence target or options are inconsistent")
	}
	return nil
}

func (f Fact) SameMount(other Fact) bool {
	return f.Validate() == nil && other.Validate() == nil &&
		f.MountID == other.MountID && f.ParentID == other.ParentID && f.Device == other.Device &&
		f.Root == other.Root && f.MountPoint == other.MountPoint && f.Filesystem == other.Filesystem &&
		f.Source == other.Source && f.MountReadOnly == other.MountReadOnly && f.KernelReadOnly == other.KernelReadOnly &&
		f.FilesystemMagic == other.FilesystemMagic && slicesEqual(f.MountOptions, other.MountOptions) &&
		slicesEqual(f.SuperOptions, other.SuperOptions)
}

func (f *Fact) Seal() error {
	if f == nil {
		return errors.New("kernel mount evidence is nil")
	}
	f.Schema = SchemaV1
	sort.Strings(f.MountOptions)
	sort.Strings(f.SuperOptions)
	f.MountOptions = compact(f.MountOptions)
	f.SuperOptions = compact(f.SuperOptions)
	f.MountReadOnly = contains(f.MountOptions, "ro")
	f.Revision = f.revision()
	return f.Validate()
}

func (f Fact) revision() string {
	copy := f
	copy.Revision = ""
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (f OverlayLabelFact) Validate() error {
	if f.Schema != OverlayLabelSchemaV1 || !canonicalAbsolute(f.Label) || f.Revision != f.revision() {
		return errors.New("overlay label evidence is malformed")
	}
	switch f.Visibility {
	case OverlayLabelVisible:
		if f.Mount.Validate() != nil {
			return errors.New("visible overlay label lacks mount evidence")
		}
	case OverlayLabelOpaque:
		if f.Mount.Schema != "" {
			return errors.New("opaque overlay label carries mount evidence")
		}
	default:
		return errors.New("overlay label visibility is invalid")
	}
	return nil
}

func (f *OverlayLabelFact) Seal() error {
	if f == nil {
		return errors.New("overlay label evidence is nil")
	}
	f.Schema = OverlayLabelSchemaV1
	f.Revision = f.revision()
	return f.Validate()
}

func (f OverlayLabelFact) revision() string {
	copy := f
	copy.Revision = ""
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (f BackingProbeFact) Validate() error {
	if f.Schema != BackingProbeSchemaV1 || !canonicalAbsolute(f.Target) || len(f.MountRevision) != sha256.Size*2 ||
		f.BytesWritten != BackingProbeBytes || !f.FileSynced ||
		!f.FilesystemSynced || !f.DirectorySynced || f.Revision != f.revision() {
		return errors.New("backing probe evidence is malformed")
	}
	switch f.Mapping {
	case BackingMappingPhysical:
		if f.ExtentCount == 0 || f.ExtentCount > MaxBackingProbeExtents || f.PhysicalBytes < f.BytesWritten {
			return errors.New("backing probe physical mapping is malformed")
		}
	case BackingMappingUnsupported:
		if f.ExtentCount != 0 || f.PhysicalBytes != 0 {
			return errors.New("unsupported backing mapping carries physical extents")
		}
	default:
		return errors.New("backing probe mapping capability is invalid")
	}
	if _, err := hex.DecodeString(f.MountRevision); err != nil {
		return errors.New("backing probe mount revision is malformed")
	}
	return nil
}

func (f *BackingProbeFact) Seal() error {
	if f == nil {
		return errors.New("backing probe evidence is nil")
	}
	f.Schema = BackingProbeSchemaV1
	f.Revision = f.revision()
	return f.Validate()
}

func (f BackingProbeFact) revision() string {
	copy := f
	copy.Revision = ""
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Parse selects the longest mount point containing target. Callers supplying
// fixtures then bind statfs facts with BindStatfs before sealing the result.
func Parse(data []byte, target string) (Fact, error) {
	target = path.Clean(target)
	if len(data) == 0 || len(data) > MaxMountInfo || !canonicalAbsolute(target) {
		return Fact{}, ErrUnavailable
	}
	var selected Fact
	found := false
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 0 || len(lines) > MaxMounts {
		return Fact{}, ErrUnavailable
	}
	for _, line := range lines {
		if line == "" || len(line) > MaxMountLine {
			return Fact{}, errors.New("mountinfo record is malformed")
		}
		fact, err := parseLine(line, target)
		if err != nil {
			return Fact{}, err
		}
		if !pathContains(fact.MountPoint, target) {
			continue
		}
		if !found || len(fact.MountPoint) > len(selected.MountPoint) || len(fact.MountPoint) == len(selected.MountPoint) && fact.MountID > selected.MountID {
			selected, found = fact, true
		}
	}
	if !found {
		return Fact{}, ErrUnavailable
	}
	return selected, nil
}

// FilesystemMountPoints returns every canonical mount point for one exact
// filesystem type after validating the complete bounded mountinfo inventory.
func FilesystemMountPoints(data []byte, filesystem string) ([]string, error) {
	if len(data) == 0 || len(data) > MaxMountInfo || filesystem == "" || len(filesystem) > 64 || strings.ContainsAny(filesystem, "\x00\r\n\t ") {
		return nil, ErrUnavailable
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 0 || len(lines) > MaxMounts {
		return nil, ErrUnavailable
	}
	points := make([]string, 0, 1)
	for _, line := range lines {
		if line == "" || len(line) > MaxMountLine {
			return nil, errors.New("mountinfo record is malformed")
		}
		fact, err := parseLine(line, "/mount-inventory")
		if err != nil {
			return nil, err
		}
		if fact.Filesystem == filesystem {
			points = append(points, fact.MountPoint)
		}
	}
	sort.Strings(points)
	return compact(points), nil
}

func BindStatfs(fact Fact, resolvedTarget string, kernelReadOnly bool, filesystemMagic int64) (Fact, error) {
	resolvedTarget = path.Clean(resolvedTarget)
	if !canonicalAbsolute(resolvedTarget) || !pathContains(fact.MountPoint, resolvedTarget) {
		return Fact{}, errors.New("statfs target differs from mount evidence")
	}
	fact.ResolvedTarget = resolvedTarget
	fact.KernelReadOnly = kernelReadOnly
	fact.FilesystemMagic = filesystemMagic
	if err := fact.Seal(); err != nil {
		return Fact{}, err
	}
	return fact, nil
}

func parseLine(line, target string) (Fact, error) {
	fields := strings.Fields(line)
	separator := -1
	for index, field := range fields {
		if field == "-" {
			separator = index
			break
		}
	}
	if separator < 6 || separator+3 >= len(fields) {
		return Fact{}, errors.New("mountinfo record is malformed")
	}
	mountID, mountErr := strconv.ParseUint(fields[0], 10, 64)
	parentID, parentErr := strconv.ParseUint(fields[1], 10, 64)
	root, rootErr := unescape(fields[3])
	mountPoint, pointErr := unescape(fields[4])
	source, sourceErr := unescape(fields[separator+2])
	if mountErr != nil || parentErr != nil || rootErr != nil || pointErr != nil || sourceErr != nil ||
		mountID == 0 || !validDevice(fields[2]) || !canonicalAbsoluteAllowRoot(root) || !canonicalAbsoluteAllowRoot(mountPoint) {
		return Fact{}, errors.New("mountinfo identity is malformed")
	}
	mountOptions, err := parseOptions(fields[5])
	if err != nil {
		return Fact{}, err
	}
	superOptions, err := parseOptions(fields[separator+3])
	if err != nil {
		return Fact{}, err
	}
	return Fact{Schema: SchemaV1, Target: target, ResolvedTarget: target, MountID: mountID, ParentID: parentID,
		Device: fields[2], Root: root, MountPoint: mountPoint, Filesystem: fields[separator+1], Source: source,
		MountOptions: mountOptions, SuperOptions: superOptions, MountReadOnly: contains(mountOptions, "ro")}, nil
}

func parseOptions(value string) ([]string, error) {
	if value == "" {
		return nil, errors.New("mountinfo options are absent")
	}
	parts := strings.Split(value, ",")
	if len(parts) > MaxOptionCount {
		return nil, errors.New("mountinfo options exceed bounds")
	}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		decoded, err := unescapeOption(part)
		if err != nil || decoded == "" || len(decoded) > 4096 || strings.ContainsAny(decoded, "\x00\r\n") {
			return nil, errors.New("mountinfo option is malformed")
		}
		result = append(result, decoded)
	}
	sort.Strings(result)
	return compact(result), nil
}

// Mount and superblock options are filesystem-specific. Unlike the pathname
// fields, Linux may emit a literal backslash in an option (WSL's 9p drvfs
// superblock is one real example). Decode kernel octal escapes when present,
// but retain other literal backslashes instead of rejecting an otherwise valid
// mount inventory.
func unescapeOption(value string) (string, error) {
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' {
			builder.WriteByte(value[index])
			continue
		}
		if index+1 >= len(value) || value[index+1] < '0' || value[index+1] > '7' {
			builder.WriteByte(value[index])
			continue
		}
		if index+3 >= len(value) || value[index+2] < '0' || value[index+2] > '7' ||
			value[index+3] < '0' || value[index+3] > '7' {
			return "", errors.New("mountinfo option escape is malformed")
		}
		parsed, _ := strconv.ParseUint(value[index+1:index+4], 8, 8)
		if parsed == 0 {
			return "", errors.New("mountinfo option contains NUL")
		}
		builder.WriteByte(byte(parsed))
		index += 3
	}
	return builder.String(), nil
}

func unescape(value string) (string, error) {
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' {
			builder.WriteByte(value[index])
			continue
		}
		if index+3 >= len(value) || value[index+1] < '0' || value[index+1] > '7' || value[index+2] < '0' || value[index+2] > '7' || value[index+3] < '0' || value[index+3] > '7' {
			return "", errors.New("mountinfo escape is malformed")
		}
		parsed, _ := strconv.ParseUint(value[index+1:index+4], 8, 8)
		if parsed == 0 {
			return "", errors.New("mountinfo value contains NUL")
		}
		builder.WriteByte(byte(parsed))
		index += 3
	}
	return builder.String(), nil
}

func canonicalAbsolute(value string) bool {
	return value != "" && value != "/" && strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsAny(value, "\x00\r\n\t")
}

func canonicalAbsoluteAllowRoot(value string) bool {
	return value == "/" || canonicalAbsolute(value)
}

func pathContains(root, value string) bool {
	return root == "/" && strings.HasPrefix(value, "/") || value == root || strings.HasPrefix(value, root+"/")
}

func validDevice(value string) bool {
	left, right, ok := strings.Cut(value, ":")
	if !ok || left == "" || right == "" {
		return false
	}
	_, leftErr := strconv.ParseUint(left, 10, 32)
	_, rightErr := strconv.ParseUint(right, 10, 32)
	return leftErr == nil && rightErr == nil
}

func contains(values []string, value string) bool {
	index := sort.SearchStrings(values, value)
	return index < len(values) && values[index] == value
}

func compact(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func sortedUniqueSafe(values []string) bool {
	if len(values) > MaxOptionCount {
		return false
	}
	for index, value := range values {
		if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
