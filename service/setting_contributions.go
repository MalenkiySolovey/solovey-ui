package service

import (
	"sort"
	"strings"
	"sync"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
)

type SettingValidator func(key string, value string, storedSecretMarker string) error

type SettingContribution struct {
	Defaults                map[string]string
	Internal                map[string]struct{}
	Encrypted               map[string]struct{}
	ClearableEmptyEncrypted map[string]struct{}
	ImportAliases           map[string]string
	Fields                  []settingsschema.Field
	Validators              []SettingValidator
}

type settingContributionEntry struct {
	name         string
	contribution SettingContribution
}

const (
	maxSettingContributions                = 128
	maxSettingKeysPerContribution          = 256
	maxSettingContributedKeys              = 2048
	maxSettingValidatorsPerContribution    = 16
	maxSettingImportAliasesPerContribution = 128
	maxSettingImportAliasBytes             = 128
)

type registeredSettingContribution struct {
	contribution SettingContribution
	token        uint64
}

var settingContributions = struct {
	sync.RWMutex
	entries   map[string]registeredSettingContribution
	nextToken uint64
}{
	entries: map[string]registeredSettingContribution{},
}

func RegisterSettingContribution(name string, contribution SettingContribution) func() {
	if name == "" {
		panic("setting contribution name is required")
	}
	ownedKeys := validateSettingContribution(name, contribution)
	settingContributions.Lock()
	defer settingContributions.Unlock()
	if _, exists := settingContributions.entries[name]; exists {
		panic("setting contribution already registered: " + name)
	}
	if len(settingContributions.entries) >= maxSettingContributions {
		panic("setting contribution registry capacity exceeded")
	}
	contributedKeys := len(ownedKeys)
	for owner, registered := range settingContributions.entries {
		contributedKeys += len(registered.contribution.Defaults)
		for key := range registered.contribution.Defaults {
			if _, duplicate := ownedKeys[key]; duplicate {
				panic("setting key already registered by " + owner + ": " + key)
			}
		}
	}
	if contributedKeys > maxSettingContributedKeys {
		panic("setting contribution key capacity exceeded")
	}
	for key := range ownedKeys {
		if _, coreOwned := defaultValueMap[key]; coreOwned {
			panic("setting contribution attempts to replace core setting: " + key)
		}
	}
	for alias := range contribution.ImportAliases {
		if _, coreOwned := defaultValueMap[alias]; coreOwned {
			panic("setting import alias conflicts with core setting: " + alias)
		}
		for owner, registered := range settingContributions.entries {
			if _, exists := registered.contribution.ImportAliases[alias]; exists {
				panic("setting import alias already registered by " + owner + ": " + alias)
			}
			if _, exists := registered.contribution.Defaults[alias]; exists {
				panic("setting import alias conflicts with setting owned by " + owner + ": " + alias)
			}
		}
	}
	for key := range ownedKeys {
		for owner, registered := range settingContributions.entries {
			if _, exists := registered.contribution.ImportAliases[key]; exists {
				panic("setting key conflicts with import alias owned by " + owner + ": " + key)
			}
		}
	}
	settingContributions.nextToken++
	token := settingContributions.nextToken
	settingContributions.entries[name] = registeredSettingContribution{
		contribution: cloneSettingContribution(contribution),
		token:        token,
	}
	return func() {
		settingContributions.Lock()
		defer settingContributions.Unlock()
		if current, ok := settingContributions.entries[name]; ok && current.token == token {
			delete(settingContributions.entries, name)
		}
	}
}

func currentSettingsSchema() settingsschema.Schema {
	return settingsschema.New(
		currentDefaultValueMap(),
		currentInternalSettingKeys(),
		currentEncryptedSettingKeys(),
		currentFieldMetadata()...,
	)
}

func currentDefaultValueMap() map[string]string {
	groups := []map[string]string{defaultValueMap}
	for _, entry := range currentSettingContributionEntries() {
		groups = append(groups, entry.contribution.Defaults)
	}
	return settingcatalog.MergeDefaultMaps(groups...)
}

func currentInternalSettingKeys() map[string]struct{} {
	groups := []map[string]struct{}{internalSettingKeys}
	for _, entry := range currentSettingContributionEntries() {
		groups = append(groups, entry.contribution.Internal)
	}
	return settingcatalog.MergeKeySets(groups...)
}

func currentEncryptedSettingKeys() map[string]struct{} {
	groups := []map[string]struct{}{encryptedSettingKeys}
	for _, entry := range currentSettingContributionEntries() {
		groups = append(groups, entry.contribution.Encrypted)
	}
	return settingcatalog.MergeKeySets(groups...)
}

func currentFieldMetadata() []settingsschema.Field {
	fields := append([]settingsschema.Field(nil), coreFieldMetadata...)
	for _, entry := range currentSettingContributionEntries() {
		fields = append(fields, entry.contribution.Fields...)
	}
	return fields
}

func currentSettingValidators() []SettingValidator {
	var validators []SettingValidator
	for _, entry := range currentSettingContributionEntries() {
		validators = append(validators, entry.contribution.Validators...)
	}
	return validators
}

func canClearEmptyEncryptedSetting(key string) bool {
	for _, entry := range currentSettingContributionEntries() {
		if _, ok := entry.contribution.ClearableEmptyEncrypted[key]; ok {
			return true
		}
	}
	return false
}

// CurrentSettingImportAliases returns component-owned mappings from external
// setting names to the canonical settings owned by active contributions.
// Importers consume this neutral snapshot instead of naming optional features.
func CurrentSettingImportAliases() map[string]string {
	aliases := map[string]string{}
	for _, entry := range currentSettingContributionEntries() {
		for alias, target := range entry.contribution.ImportAliases {
			aliases[alias] = target
		}
	}
	return aliases
}

func currentSettingContributionEntries() []settingContributionEntry {
	settingContributions.RLock()
	defer settingContributions.RUnlock()
	entries := make([]settingContributionEntry, 0, len(settingContributions.entries))
	for name, registered := range settingContributions.entries {
		entries = append(entries, settingContributionEntry{
			name:         name,
			contribution: cloneSettingContribution(registered.contribution),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}

func cloneSettingContribution(contribution SettingContribution) SettingContribution {
	return SettingContribution{
		Defaults:                cloneSettingStringMap(contribution.Defaults),
		Internal:                cloneSettingKeySet(contribution.Internal),
		Encrypted:               cloneSettingKeySet(contribution.Encrypted),
		ClearableEmptyEncrypted: cloneSettingKeySet(contribution.ClearableEmptyEncrypted),
		ImportAliases:           cloneSettingStringMap(contribution.ImportAliases),
		Fields:                  append([]settingsschema.Field(nil), contribution.Fields...),
		Validators:              append([]SettingValidator(nil), contribution.Validators...),
	}
}

func validateSettingContribution(name string, contribution SettingContribution) map[string]struct{} {
	if len(contribution.Defaults) > maxSettingKeysPerContribution {
		panic("setting contribution has too many keys: " + name)
	}
	if len(contribution.Validators) > maxSettingValidatorsPerContribution {
		panic("setting contribution has too many validators: " + name)
	}
	owned := make(map[string]struct{}, len(contribution.Defaults))
	for key := range contribution.Defaults {
		if !validSettingContributionKey(key) {
			panic("setting contribution has invalid key: " + name)
		}
		owned[key] = struct{}{}
	}
	for _, group := range []map[string]struct{}{
		contribution.Internal,
		contribution.Encrypted,
		contribution.ClearableEmptyEncrypted,
	} {
		for key := range group {
			if _, ok := owned[key]; !ok {
				panic("setting contribution metadata targets an unowned setting: " + name + ": " + key)
			}
		}
	}
	for key := range contribution.ClearableEmptyEncrypted {
		if _, encrypted := contribution.Encrypted[key]; !encrypted {
			panic("clearable setting is not encrypted: " + name + ": " + key)
		}
	}
	seenFields := map[string]struct{}{}
	for _, field := range contribution.Fields {
		if _, ok := owned[field.Key]; !ok {
			panic("setting field targets an unowned setting: " + name + ": " + field.Key)
		}
		if _, duplicate := seenFields[field.Key]; duplicate {
			panic("setting contribution repeats field metadata: " + name + ": " + field.Key)
		}
		seenFields[field.Key] = struct{}{}
	}
	if len(contribution.ImportAliases) > maxSettingImportAliasesPerContribution {
		panic("setting contribution has too many import aliases: " + name)
	}
	for alias, target := range contribution.ImportAliases {
		if !validSettingContributionKey(alias) || !validSettingContributionKey(target) {
			panic("setting contribution has invalid import alias: " + name)
		}
		if _, ok := owned[target]; !ok {
			panic("setting contribution import alias targets an unowned setting: " + name + ": " + target)
		}
	}
	return owned
}

func validSettingContributionKey(key string) bool {
	return key != "" && len(key) <= maxSettingImportAliasBytes && strings.TrimSpace(key) == key && !strings.ContainsAny(key, " \t\r\n\x00")
}

func cloneSettingStringMap(values map[string]string) map[string]string {
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func cloneSettingKeySet(values map[string]struct{}) map[string]struct{} {
	copied := make(map[string]struct{}, len(values))
	for key := range values {
		copied[key] = struct{}{}
	}
	return copied
}
