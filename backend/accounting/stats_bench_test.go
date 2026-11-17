package accounting

import (
	entitytypes "github.com/MalenkiySolovey/solovey-ui/backend/entityidentity"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"testing"
)

func BenchmarkUpdateClientTrafficDeltas(b *testing.B) {
	for _, emptyPercent := range []int{0, 50, 90} {
		emptyPercent := emptyPercent
		b.Run(fmt.Sprintf("clients_1000_empty_%d_pct", emptyPercent), func(b *testing.B) {
			initSettingTestDB(b)
			const clients = 1000
			rows := make([]model.Client, clients)
			for i := range rows {
				rows[i] = model.Client{Id: uint(i + 1)}
			}
			if err := dbsqlite.CreateInBatches(dbsqlite.DB(), &rows); err != nil {
				b.Fatal(err)
			}
			deltas := make(map[entitytypes.VPNUserID]clientTrafficDelta, clients)
			for i := 0; i < clients; i++ {
				name := entitytypes.VPNUserID(i + 1)
				if i%100 < emptyPercent {
					deltas[name] = clientTrafficDelta{}
					continue
				}
				deltas[name] = clientTrafficDelta{up: int64(i + 1), down: int64(i + 2)}
			}
			b.ReportMetric(float64(emptyPercent), "empty_pct")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tx := dbsqlite.DB().Begin()
				if tx.Error != nil {
					b.Fatal(tx.Error)
				}
				if err := updateClientTrafficDeltas(tx, deltas); err != nil {
					_ = tx.Rollback().Error
					b.Fatal(err)
				}
				if err := tx.Rollback().Error; err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
