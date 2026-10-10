package ssmcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

func NamespaceKey(name string) string {
	name = filepath.Clean(name)
	if relative, err := filepath.Rel(Root(), name); err == nil && isRestoreSeed(relative) {
		name += ".state"
	}
	if runtime.GOOS == "windows" {
		name = strings.ToLower(name)
	}
	return name
}

func isRestoreSeed(relative string) bool {
	if runtime.GOOS == "windows" {
		relative = strings.ToLower(relative)
	}
	return strings.HasPrefix(relative, "restored"+string(filepath.Separator)) && filepath.Base(relative) == "state.seed"
}

func Path(raw []byte) (string, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return "", errors.New("SSM_CACHE_OPTIONS_INVALID")
	}
	if field, exists := fields["cache_path"]; exists {
		var name string
		if json.Unmarshal(field, &name) != nil {
			return "", errors.New("SSM_CACHE_OPTIONS_INVALID")
		}
		return name, nil
	}
	return "", nil
}

func OptionsFindings(path string, raw []byte) []diagnostics.Finding {
	name, err := Path(raw)
	code := "SSM_CACHE_PATH_REJECTED"
	if err == nil && name != "" {
		var store *Store
		store, err = New(name)
		if err == nil {
			_, err = store.Read(context.Background())
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
			if err != nil {
				code = "SSM_CACHE_CONTENT_REJECTED"
			}
		}
	}
	if err == nil {
		return nil
	}
	return []diagnostics.Finding{{Kind: "service-cache", Path: path + ".cache_path", Code: code, Severity: diagnostics.Error,
		Message: "SSM credentials require bounded valid state in a private regular cache below the deployment data directory's ssm owner folder. Preserve the old state and correct its owned path or cache contents before retrying.", MigrationOutcome: diagnostics.ManualRequired, OperatorActionRequired: true}}
}

func ConfigFindings(source []byte) []diagnostics.Finding {
	var root struct {
		Services []json.RawMessage `json:"services"`
	}
	if json.Unmarshal(source, &root) != nil {
		return nil
	}
	result := []diagnostics.Finding{}
	seen := map[string]bool{}
	for i, raw := range root.Services {
		var service struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &service) != nil || service.Type != "ssm-api" {
			continue
		}
		path := fmt.Sprintf("services[%d]", i)
		result = append(result, OptionsFindings(path, raw)...)
		name, err := Path(raw)
		if err == nil && name != "" {
			name = NamespaceKey(name)
			if seen[name] {
				result = append(result, diagnostics.Finding{Kind: "service-cache", Path: path + ".cache_path", Code: "SSM_CACHE_SHARED_PATH_REJECTED", Severity: diagnostics.Error, Message: "Each enabled SSM cache has one service owner; sharing a cache path is not authorized.", MigrationOutcome: diagnostics.ManualRequired, OperatorActionRequired: true})
			}
			seen[name] = true
		}
	}
	return result
}
