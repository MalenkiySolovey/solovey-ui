package outbounds

import (
	"testing"

	"gorm.io/gorm"
)

func TestHookStaleCleanupPreservesNewRegistration(t *testing.T) {
	oldDelete := RegisterDeleteHook("test.generation", func(*gorm.DB, string) error { return nil })
	oldDelete()
	newDelete := RegisterDeleteHook("test.generation", func(*gorm.DB, string) error { return nil })
	t.Cleanup(newDelete)
	oldDelete()
	if entries := deleteHookSnapshot(); len(entries) != 1 {
		t.Fatalf("stale delete-hook cleanup changed current registry: %#v", entries)
	}

	oldMetadata := RegisterMetadataAnnotator("test.generation", func(*gorm.DB, []map[string]interface{}) error { return nil })
	oldMetadata()
	newMetadata := RegisterMetadataAnnotator("test.generation", func(*gorm.DB, []map[string]interface{}) error { return nil })
	t.Cleanup(newMetadata)
	oldMetadata()
	if entries := metadataAnnotatorSnapshot(); len(entries) != 1 {
		t.Fatalf("stale metadata cleanup changed current registry: %#v", entries)
	}
}
