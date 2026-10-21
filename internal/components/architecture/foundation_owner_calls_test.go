package architecture

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestFoundationSemanticOutboundOwnerCalls(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		rawWrites    int
	}{
		{"owner", `package example; import owner "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"; import "gorm.io/gorm"; func write(tx *gorm.DB) { _, _ = owner.Save(tx, "new", nil) }`, 0},
		{"alias", `package example; import canonical "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"; import "gorm.io/gorm"; func write(tx *gorm.DB) { _, _ = canonical.Save(tx, "edit", nil) }`, 0},
		{"raw", `package example; import "gorm.io/gorm"; func write(tx *gorm.DB) { tx.Save(nil) }`, 1},
		{"shadow", `package example; import owner "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"; import "gorm.io/gorm"; func write(owner *gorm.DB) { owner.Save(nil) }`, 1},
		{"unrelated", `package example; import owner "example.org/unrelated"; import "gorm.io/gorm"; func write(tx *gorm.DB) { owner.Save(tx, nil) }`, 1},
		{"mixed", `package example; import owner "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"; import "gorm.io/gorm"; func write(tx *gorm.DB) { _, _ = owner.Save(tx, "new", nil); tx.Save(nil) }`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "owner.go", tc.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			sites, _ := inspectFoundationFile("components/import-xui/database/owner.go", file)
			count := 0
			for _, site := range sites {
				if site.Rule == "component-model-mutation" {
					count++
				}
			}
			if count != tc.rawWrites {
				t.Fatalf("raw mutation sites=%d, want %d", count, tc.rawWrites)
			}
		})
	}
}
