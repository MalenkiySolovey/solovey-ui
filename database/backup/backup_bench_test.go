package backup

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

func BenchmarkInstalledOwnerBackupManifest(b *testing.B) {
	manifest := BackupManifest{
		Schema: BackupManifestSchema, AppVersion: "2026.2.3", CoreSchema: "1.11",
		SQLiteModule: "v1.14.49", SQLiteRuntime: "3.53.4",
		SQLiteSourceID: "2026-07-24 19:02:57 fixture", Compatibility: "EXACT_OR_FORWARD_MIGRATABLE",
		Owners: make([]BackupOwnerManifest, 256), Tables: make([]BackupTableManifest, MaxBackupTables),
	}
	for index := range manifest.Owners {
		manifest.Owners[index] = BackupOwnerManifest{
			ID: fmt.Sprintf("owner-%03d", index), Installed: true, Available: true, Mode: "NO_DURABLE_DATA",
		}
	}
	for index := range manifest.Tables {
		digest := benchmarkTableDigest(index)
		manifest.Tables[index] = BackupTableManifest{
			Owner: "owner-000", Name: fmt.Sprintf("table_%03d", index), SchemaDigest: digest,
			ContentDigest: digest,
		}
	}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if backupManifestDigest(manifest) == "" {
			b.Fatal("manifest digest is empty")
		}
	}
}

func BenchmarkRestoreRehearsal(b *testing.B) {
	fixture := newLegacyBackup(b)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		result, err := Rehearse(context.Background(), bytes.NewReader(fixture))
		if err != nil || !result.Possible {
			b.Fatalf("rehearsal=%#v err=%v", result, err)
		}
	}
}

func benchmarkTableDigest(index int) string {
	schema, _ := excludedTableDigests(fmt.Sprintf("table_%03d", index), "NONPORTABLE_RUNTIME_STATE")
	return schema
}
