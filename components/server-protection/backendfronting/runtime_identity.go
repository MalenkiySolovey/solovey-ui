package backendfronting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	fronting "github.com/MalenkiySolovey/solovey-component-server-protection/service/fronting"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	helper "github.com/MalenkiySolovey/solovey-component-server-protection/service/helpercontract"
)

type NginxRuntimeInspectorV2 struct {
	Config fronting.NginxRuntimeInspectionConfigV2
	Reader fronting.NginxVersionReaderV2
}

func (i NginxRuntimeInspectorV2) Inspect(ctx context.Context) (fronting.NginxRuntimeIdentityV2, error) {
	if err := ctx.Err(); err != nil {
		return fronting.NginxRuntimeIdentityV2{}, err
	}
	identity := fronting.NewNginxRuntimeIdentityV2(i.Config)
	paths, pathReasons := inspectExecutableCandidates(i.Config.CandidatePaths, i.Config.AllowedExecutableRoots)
	if len(paths) == 0 {
		identity.State = fronting.NginxNotInstalled
		identity.ReasonCodes = append(pathReasons, "nginx_not_installed")
		return fronting.FinalizeNginxRuntimeIdentityV2(identity), nil
	}
	if len(paths) != 1 || len(pathReasons) != 0 {
		identity.ReasonCodes = append(pathReasons, "nginx_executable_ambiguous")
		return fronting.FinalizeNginxRuntimeIdentityV2(identity), nil
	}
	selected := paths[0]
	before, err := os.Stat(selected)
	if err != nil || !before.Mode().IsRegular() {
		identity.ReasonCodes = append(identity.ReasonCodes, "nginx_executable_non_regular")
		return fronting.FinalizeNginxRuntimeIdentityV2(identity), nil
	}
	digest, err := digestFileBounded(selected)
	if err != nil {
		identity.ReasonCodes = append(identity.ReasonCodes, "nginx_executable_identity_unknown")
		return fronting.FinalizeNginxRuntimeIdentityV2(identity), nil
	}
	identity.ExecutableDigest = digest
	identity.ExecutableDevice, identity.ExecutableInode = platformFileIdentityV2(before)
	identity.PlatformIdentityRevision = fronting.NginxRuntimeFactRevisionV2(struct {
		Size    int64
		Mode    fs.FileMode
		ModTime int64
		Device  uint64
		Inode   uint64
	}{before.Size(), before.Mode(), before.ModTime().UnixNano(), identity.ExecutableDevice, identity.ExecutableInode})
	identity.ExecutableIdentityRevision = fronting.NginxRuntimeFactRevisionV2(struct {
		Digest, Platform string
		Device, Inode    uint64
	}{digest, identity.PlatformIdentityRevision, identity.ExecutableDevice, identity.ExecutableInode})
	if i.Reader == nil {
		identity.ReasonCodes = append(identity.ReasonCodes, "nginx_version_reader_unavailable")
		return fronting.FinalizeNginxRuntimeIdentityV2(identity), nil
	}
	observation, readErr := i.Reader.ReadNginxVersion(ctx, selected)
	if readErr != nil {
		if err := ctx.Err(); err != nil {
			return fronting.NginxRuntimeIdentityV2{}, err
		}
		identity.ReasonCodes = append(identity.ReasonCodes, "nginx_version_unparseable")
		return fronting.FinalizeNginxRuntimeIdentityV2(identity), nil
	}
	if err := ctx.Err(); err != nil {
		return fronting.NginxRuntimeIdentityV2{}, err
	}
	after, statErr := os.Stat(selected)
	afterDigest, digestErr := digestFileBounded(selected)
	if statErr != nil || digestErr != nil || before.Size() != after.Size() || before.ModTime() != after.ModTime() || digest != afterDigest {
		identity.ReasonCodes = append(identity.ReasonCodes, "nginx_executable_identity_drift")
		return fronting.FinalizeNginxRuntimeIdentityV2(identity), nil
	}
	identity, valid := fronting.ApplyNginxBuildObservationV2(identity, observation, i.Config.LoadedDynamicModules)
	if !valid {
		return fronting.FinalizeNginxRuntimeIdentityV2(identity), nil
	}
	rootRevision, ownershipRevision, ownershipOK := inspectManagedContourV2(i.Config.ManagedRootPath, i.Config.ControlledConfigPath)
	identity.ManagedRootIdentityRevision, identity.LoaderConfigOwnershipRevision = rootRevision, ownershipRevision
	if i.Config.InstallationClass == fronting.NginxInstallationExternal {
		identity.State = fronting.NginxExternalManaged
		identity.ReasonCodes = append(identity.ReasonCodes, "nginx_external_managed")
		return fronting.FinalizeNginxRuntimeIdentityV2(identity), nil
	}
	if i.Config.InstallationClass != fronting.NginxInstallationManaged || !ownershipOK {
		identity.ReasonCodes = append(identity.ReasonCodes, "nginx_managed_contour_unknown")
		return fronting.FinalizeNginxRuntimeIdentityV2(identity), nil
	}
	return fronting.FinalizeNginxRuntimeIdentityV2(identity), nil
}

func inspectExecutableCandidates(candidates, roots []string) ([]string, []string) {
	allowed := make([]string, 0, len(roots))
	for _, root := range roots {
		if absolute, err := filepath.Abs(root); err == nil {
			if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
				allowed = append(allowed, filepath.Clean(resolved))
			}
		}
	}
	seen := map[string]struct{}{}
	reasons := []string{}
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(strings.TrimSpace(candidate))
		if err != nil || !filepath.IsAbs(absolute) {
			reasons = append(reasons, "nginx_executable_path_escape")
			continue
		}
		info, err := os.Lstat(absolute)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			reasons = append(reasons, "nginx_executable_identity_unknown")
			continue
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil || !withinAnyRootV2(resolved, allowed) {
			reasons = append(reasons, "nginx_executable_path_escape")
			continue
		}
		resolvedInfo, err := os.Stat(resolved)
		if err != nil || !resolvedInfo.Mode().IsRegular() || info.IsDir() {
			reasons = append(reasons, "nginx_executable_non_regular")
			continue
		}
		seen[filepath.Clean(resolved)] = struct{}{}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, fronting.CanonicalNginxRuntimeReasonsV2(reasons)
}

func withinAnyRootV2(path string, roots []string) bool {
	clean := filepath.Clean(path)
	for _, root := range roots {
		relative, err := filepath.Rel(root, clean)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return true
		}
	}
	return false
}

func digestFileBounded(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() < 0 || info.Size() > 256<<20 {
		return "", errors.New("nginx executable size is outside the bounded identity contract")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func inspectManagedContourV2(root, config string) (string, string, bool) {
	root = strings.TrimSpace(root)
	config = strings.TrimSpace(config)
	if root == "" || config == "" {
		return "", "", false
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", false
	}
	rootInfo, err := os.Stat(rootResolved)
	if err != nil || !rootInfo.IsDir() {
		return "", "", false
	}
	configResolved, err := filepath.EvalSymlinks(config)
	if err != nil || !withinAnyRootV2(configResolved, []string{rootResolved}) {
		return "", "", false
	}
	configInfo, err := os.Stat(configResolved)
	if err != nil || !configInfo.Mode().IsRegular() {
		return "", "", false
	}
	rootDevice, rootInode := platformFileIdentityV2(rootInfo)
	rootRevision := fronting.NginxRuntimeFactRevisionV2(struct {
		ResolvedPathRevision string
		Mode                 fs.FileMode
		ModTime              int64
		Device               uint64
		Inode                uint64
	}{fronting.NginxRuntimeFactRevisionV2(filepath.Clean(rootResolved)), rootInfo.Mode(), rootInfo.ModTime().UnixNano(), rootDevice, rootInode})
	configDigest, err := digestFileBounded(configResolved)
	if err != nil {
		return "", "", false
	}
	configDevice, configInode := platformFileIdentityV2(configInfo)
	configRevision := fronting.NginxRuntimeFactRevisionV2(struct {
		ResolvedPathRevision string
		Digest               string
		Size                 int64
		Mode                 fs.FileMode
		ModTime              int64
		Device               uint64
		Inode                uint64
	}{fronting.NginxRuntimeFactRevisionV2(filepath.Clean(configResolved)), configDigest, configInfo.Size(), configInfo.Mode(), configInfo.ModTime().UnixNano(), configDevice, configInode})
	return rootRevision, fronting.NginxRuntimeFactRevisionV2(struct{ Root, Config string }{rootRevision, configRevision}), true
}

// RuntimeInspector binds the restricted helper observation to the existing OS inspector.
type RuntimeInspector struct {
	helper     fronting.Helper
	instanceID string
}

func NewRuntimeInspector(workflow *fronting.Workflow) RuntimeInspector {
	if workflow == nil || workflow.Helper == nil || workflow.Manager == nil {
		return RuntimeInspector{}
	}
	return RuntimeInspector{helper: workflow.Helper, instanceID: workflow.Manager.InstanceID()}
}

func (owner RuntimeInspector) Inspect(ctx context.Context, config fronting.NginxRuntimeInspectionConfigV2, observation fronting.NginxVersionObservationV2) (fronting.NginxRuntimeIdentityV2, error) {
	if err := ctx.Err(); err != nil {
		return fronting.NginxRuntimeIdentityV2{}, err
	}
	if owner.helper == nil || owner.instanceID == "" {
		return fronting.NginxRuntimeIdentityV2{}, errors.New("nginx_runtime_inspection_owner_unavailable")
	}
	response, err := owner.helper.Execute(ctx, helper.Request{
		ProtocolVersion: helper.ProtocolVersion,
		Correlation:     helper.Correlation{OperationID: "capabilities", InstanceID: owner.instanceID},
		Operation:       helper.OperationCapabilities, Capabilities: &helper.CapabilitiesRequest{},
	})
	if err != nil {
		return fronting.NginxRuntimeIdentityV2{}, err
	}
	capabilities := response.Capabilities
	if !response.OK || capabilities == nil || capabilities.Revision == "" || !capabilities.Nginx.PlatformKnown || !capabilities.Nginx.Linux || !capabilities.Nginx.Available {
		return fronting.NginxRuntimeIdentityV2{}, errors.New("nginx_runtime_inspection_capability_unknown")
	}
	binary := capabilities.Nginx.Binary.TargetPath
	if binary == "" {
		binary = capabilities.Nginx.Binary.Path
	}
	// A feature-supplied path is never filesystem authority. Admit only the
	// current restricted helper's exact executable and managed contour, fenced
	// by its capability and active revisions.
	if binary == "" || !filepath.IsAbs(binary) || len(config.CandidatePaths) != 1 || config.CandidatePaths[0] != binary ||
		len(config.AllowedExecutableRoots) != 1 || config.AllowedExecutableRoots[0] != filepath.Dir(binary) ||
		config.ManagedRootPath != capabilities.Nginx.ManagedRoot || config.ControlledConfigPath != capabilities.Nginx.ControlledConfig ||
		config.InstallationClass != fronting.NginxInstallationManaged || config.HelperContractRevision != capabilities.Revision ||
		config.ActiveManagedRevision != capabilities.Nginx.ActiveRevision || config.HelperProtocolVersion != helper.ProtocolVersion ||
		config.HelperVersion != capabilities.HelperVersion || config.HelperContractVersion != capabilities.ContractVersion {
		return fronting.NginxRuntimeIdentityV2{}, errors.New("nginx_runtime_inspection_authority_changed")
	}
	return (NginxRuntimeInspectorV2{Config: config, Reader: staticVersionReader{observation}}).Inspect(ctx)
}

type staticVersionReader struct {
	observation fronting.NginxVersionObservationV2
}

func (r staticVersionReader) ReadNginxVersion(context.Context, string) (fronting.NginxVersionObservationV2, error) {
	return r.observation, nil
}
