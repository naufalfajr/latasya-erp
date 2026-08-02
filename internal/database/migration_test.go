package database_test

import (
	"path/filepath"
	"testing"

	latasyaerp "github.com/naufal/latasya-erp"
	"github.com/naufal/latasya-erp/internal/database"
)

func TestMigrationsPreserveHistoricalDuplicateJournalReferences(t *testing.T) {
	database.SetMigrations(latasyaerp.MigrationFS)
	db, err := database.Open(filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Seed(db); err != nil {
		t.Fatal(err)
	}
	var userID int
	if err := db.QueryRow("SELECT id FROM users WHERE username='admin'").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := db.Exec("INSERT INTO journal_entries(entry_date,reference,description,created_by) VALUES(?,?,?,?)", "2026-05-01", "JE-202605-0077", "historical duplicate", userID); err != nil {
			t.Fatalf("historical duplicate reference rejected: %v", err)
		}
	}
}
