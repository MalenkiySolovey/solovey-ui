package service

import (
	"sort"
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
	Fields                  []settingsschema.Field
	Validators              []SettingValidator
}

type settingContributionEntry struct {
	name         string
	contribution SettingContribution
}

var settingContributions = struct {
	sync.RWMutex
	entries map[string]SettingContribution
}{
	entries: map[string]SettingContribution{},
}

func RegisterSettingContribution(name string, contribution SettingContribution) func() {
	if name == "" {
		panic("setting contribution name is required")
	}
	settingContributions.Lock()
	defer settingContributions.Unlock()
	if _, exists := settingContributions.entries[name]; exists {
		panic("setting contribution already registered: " + name)
	}
	settingContributions.entries[name] = cloneSettingContribution(contribution)
	return func() {
		settingContributions.Lock()
		defer settingContributions.Unlock()
		delete(settingContributions.entries, name)
	}
}

func resetSettingContributionsForTest() {
	settingContributions.Lock()
	defer settingContributions.Unlock()
	settingContributions.entries = map[string]SettingContribution{}
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

func currentSettingContributionEntries() []settingContributionEntry {
	settingContributions.RLock()
	defer settingContributions.RUnlock()
	entries := make([]settingContributionEntry, 0, len(settingContributions.entries))
	for name, contribution := range settingContributions.entries {
		entries = append(entries, settingContributionEntry{
			name:         name,
			contribution: cloneSettingContribution(contribution),
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
		Fields:                  append([]settingsschema.Field(nil), contribution.Fields...),
		Validators:              append([]SettingValidator(nil), contribution.Validators...),
	}
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
