package audit_test

import (
	"context"
	"testing"

	latasyaerp "github.com/naufal/latasya-erp"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/database"
)

func BenchmarkLogFileSQLite(b *testing.B) {
	database.SetMigrations(latasyaerp.MigrationFS)
	db, err := database.Open(b.TempDir() + "/audit.db")
	if err != nil {
		b.Fatalf("open benchmark database: %v", err)
	}
	b.Cleanup(func() { db.Close() })

	event := audit.Event{
		Action:      "invoice.create",
		TargetType:  "invoice",
		TargetID:    42,
		TargetLabel: "INV-202608-0042",
		Metadata: map[string]any{
			"contact_id": 7,
			"total":      1_500_000,
			"line_count": 2,
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		audit.Log(context.Background(), db, event)
	}
}
