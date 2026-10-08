package entitytls

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/caddyserver/certmagic"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/service/filemanager"
)

// ProviderFiles describes only persistent keys belonging to this ACME issuer,
// account and configured domains. Transient locks, challenges, OCSP caches and
// unrelated host files are outside the certificate owner's backup inventory.
type ProviderFiles struct {
	Tag              string
	Root             string
	AccountPrefix    string
	AccountFolder    string
	AccountDiscovery bool
	Groups           [][]string
}

func ProviderFileInventory(ctx context.Context, definition ProviderDefinition) (ProviderFiles, error) {
	if err := ValidateProviderDefinition(definition, false); err != nil {
		return ProviderFiles{}, err
	}
	var options struct {
		Provider   string                     `json:"provider"`
		Directory  string                     `json:"data_directory"`
		Email      string                     `json:"email"`
		AccountKey string                     `json:"account_key"`
		Domain     badoption.Listable[string] `json:"domain"`
	}
	if json.Unmarshal(definition.Options, &options) != nil {
		return ProviderFiles{}, errors.New("TLS_PROVIDER_FILE_OPTIONS_INVALID")
	}
	authority, err := ACMEAuthority(options.Provider)
	if err != nil {
		return ProviderFiles{}, err
	}
	root := filemanager.BasePath(ctx, os.ExpandEnv(options.Directory))
	if options.Directory == "" {
		storage, ok := certmagic.Default.Storage.(*certmagic.FileStorage)
		if !ok || storage == nil {
			return ProviderFiles{}, errors.New("TLS_PROVIDER_STORAGE_UNAVAILABLE: file backup requires the accepted file storage adapter")
		}
		root = storage.Path
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return ProviderFiles{}, errors.New("TLS_PROVIDER_STORAGE_UNAVAILABLE")
	}
	issuer := (&certmagic.ACMEIssuer{CA: authority}).IssuerKey()
	email := strings.ToLower(strings.TrimSpace(options.Email))
	if email == "" {
		email = strings.ToLower(strings.TrimSpace(certmagic.DefaultACME.Email))
	}
	account := email
	if account == "" {
		account = "default"
	}
	username := account
	if at := strings.IndexByte(account, '@'); at >= 0 {
		if at == 0 {
			username = account[1:]
		} else {
			username = account[:at]
		}
	}
	folder := certmagic.StorageKeys.Safe(account)
	prefix := path.Join("acme", certmagic.StorageKeys.Safe(issuer), "users")
	registration, private := username, username
	if registration == "" {
		registration = "registration"
		private = "private"
	}
	result := ProviderFiles{Tag: definition.Tag, Root: root, AccountPrefix: prefix, AccountFolder: folder, AccountDiscovery: email == "" || options.AccountKey != ""}
	result.Groups = append(result.Groups, []string{path.Join(prefix, folder, certmagic.StorageKeys.Safe(registration)+".json"), path.Join(prefix, folder, certmagic.StorageKeys.Safe(private)+".key")})
	seen := map[string]bool{}
	if len(options.Domain) > 1024 {
		return ProviderFiles{}, errors.New("TLS_PROVIDER_FILE_INVENTORY_EXCESSIVE")
	}
	for _, domain := range options.Domain {
		key := certmagic.StorageKeys.SiteCert(issuer, domain)
		if seen[key] {
			continue
		}
		seen[key] = true
		result.Groups = append(result.Groups, []string{key, certmagic.StorageKeys.SitePrivateKey(issuer, domain), certmagic.StorageKeys.SiteMeta(issuer, domain)})
	}
	return result, nil
}

// RebindProviderDirectory changes only the storage adapter after its private
// files have been staged by the existing certificate owner.
func RebindProviderDirectory(definition ProviderDefinition, directory string) (ProviderDefinition, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(definition.Options, &fields) != nil || fields == nil {
		return definition, errors.New("TLS_PROVIDER_FILE_OPTIONS_INVALID")
	}
	fields["data_directory"], _ = json.Marshal(directory)
	definition.Options, _ = json.Marshal(fields)
	return definition, nil
}
