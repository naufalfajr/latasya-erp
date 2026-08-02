package documentnumber_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/documentnumber"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestGenerateDocNumberContextClaimsMonthlySequence(t *testing.T) {
	db := testutil.SetupTestDB(t)
	at := time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)
	prefix := "JE-202608-"

	first, err := documentnumber.NextAt(context.Background(), db, documentnumber.JournalEntry, at)
	if err != nil {
		t.Fatal(err)
	}
	second, err := documentnumber.NextAt(context.Background(), db, documentnumber.JournalEntry, at)
	if err != nil {
		t.Fatal(err)
	}
	if first != prefix+"0001" || second != prefix+"0002" {
		t.Fatalf("numbers=(%q,%q)", first, second)
	}
}

func TestGenerateDocNumberContextRejectsUnknownTarget(t *testing.T) {
	db := testutil.SetupTestDB(t)
	_, err := documentnumber.Next(context.Background(), db, documentnumber.Kind("unknown"))
	if err == nil || !strings.Contains(err.Error(), "invalid document number kind") {
		t.Fatalf("error=%v", err)
	}
}
