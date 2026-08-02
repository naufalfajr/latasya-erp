package contact_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

var (
	portalIssuer  = contact.PortalIssuer{UserID: 1, CanIssue: true}
	portalManager = contact.Actor{UserID: 1, CanManagePortal: true}
)

func TestEnsurePortalCodeRequiresAuthorizedActor(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := contact.New(db)
	created, err := m.Create(context.Background(), contact.Actor{UserID: 1, CanManage: true}, contact.Draft{Name: "Portal Auth", ContactType: "customer", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.EnsurePortalCode(context.Background(), contact.PortalIssuer{}, created.ID)
	if !errors.Is(err, contact.ErrForbidden) {
		t.Fatalf("EnsurePortalCode error = %v, want ErrForbidden", err)
	}
}

func TestContactsByPortalCode_GroupsSiblingsBySharedPhone(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	shared := "083333333333"
	c1 := &model.Contact{Name: "Sibling One", ContactType: "customer", Phone: shared, IsActive: true}
	c2 := &model.Contact{Name: "Sibling Two", ContactType: "customer", Phone: shared, IsActive: true}
	testutil.CreateContact(db, c1)
	testutil.CreateContact(db, c2)
	contacts, _ := testutil.ListContacts(db, testutil.ContactFilter{Search: "Sibling"})
	if len(contacts) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(contacts))
	}

	code, err := contact.New(db).EnsurePortalCode(context.Background(), portalIssuer, contacts[0].ID)
	if err != nil {
		t.Fatalf("get code: %v", err)
	}

	fam, err := contact.New(db).FamilyByPortalCode(context.Background(), code)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if fam == nil || len(fam.Contacts) != 2 {
		t.Fatalf("expected family of 2, got %+v", fam)
	}
}

// TestContactsByPortalCode_GroupsSiblingsByDifferentlyFormattedPhone guards
// against comparing phone numbers as raw strings: "081..." and "+62 812-..."
// are the same number, entered inconsistently, and must still group.
func TestContactsByPortalCode_GroupsSiblingsByDifferentlyFormattedPhone(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	c1 := &model.Contact{Name: "Format One", ContactType: "customer", Phone: "081234567890", IsActive: true}
	c2 := &model.Contact{Name: "Format Two", ContactType: "customer", Phone: "+62 812-3456-7890", IsActive: true}
	testutil.CreateContact(db, c1)
	testutil.CreateContact(db, c2)
	contacts, _ := testutil.ListContacts(db, testutil.ContactFilter{Search: "Format"})
	if len(contacts) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(contacts))
	}

	code, err := contact.New(db).EnsurePortalCode(context.Background(), portalIssuer, contacts[0].ID)
	if err != nil {
		t.Fatalf("get code: %v", err)
	}

	fam, err := contact.New(db).FamilyByPortalCode(context.Background(), code)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if fam == nil || len(fam.Contacts) != 2 {
		t.Fatalf("expected differently formatted phone numbers to group into a family of 2, got %+v", fam)
	}
}

func TestContactsByPortalCode_BlankPhoneDoesNotGroup(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	c1 := &model.Contact{Name: "No Phone One", ContactType: "customer", Phone: "", IsActive: true}
	c2 := &model.Contact{Name: "No Phone Two", ContactType: "customer", Phone: "", IsActive: true}
	testutil.CreateContact(db, c1)
	testutil.CreateContact(db, c2)
	contacts, _ := testutil.ListContacts(db, testutil.ContactFilter{Search: "No Phone"})
	if len(contacts) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(contacts))
	}

	code, err := contact.New(db).EnsurePortalCode(context.Background(), portalIssuer, contacts[0].ID)
	if err != nil {
		t.Fatalf("get code: %v", err)
	}

	fam, err := contact.New(db).FamilyByPortalCode(context.Background(), code)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if fam == nil || len(fam.Contacts) != 1 {
		t.Fatalf("blank-phone contact should not group with others, got %+v", fam)
	}
}

func TestContactsByPortalCode_UnknownCodeReturnsNil(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	fam, err := contact.New(db).FamilyByPortalCode(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if fam != nil {
		t.Errorf("expected nil family for unknown code, got %+v", fam)
	}
}

func TestPortalCode_FormatAndPrefix(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	tests := []struct {
		name       string
		wantPrefix string
	}{
		{"Andi Wijaya", "andi-"},
		{"budi", "budi-"},
		{"Muhammad Rizki", "muhammad-"},          // first word only, not truncated at 6
		{"Abdurrahman Saputra", "abdurrahman-"},  // 11 letters, still intact
		{"Bartholomewmagnus X", "bartholomewm-"}, // 12-char cap finally bites
		{"123", "lts-"},                          // no letters to work with
		{"  ", "lts-"},
	}

	for _, tt := range tests {
		if _, err := db.Exec("INSERT INTO contacts (name,contact_type,is_active) VALUES (?,'customer',1)", tt.name); err != nil {
			t.Fatalf("create contact %q: %v", tt.name, err)
		}
		var id int
		if err := db.QueryRow("SELECT id FROM contacts ORDER BY id DESC LIMIT 1").Scan(&id); err != nil {
			t.Fatalf("read back contact %q: %v", tt.name, err)
		}

		code, err := contact.New(db).EnsurePortalCode(context.Background(), portalIssuer, id)
		if err != nil {
			t.Fatalf("generate code for %q: %v", tt.name, err)
		}
		if !strings.HasPrefix(code, tt.wantPrefix) {
			t.Errorf("code for %q = %q, want prefix %q", tt.name, code, tt.wantPrefix)
		}
		digits := strings.TrimPrefix(code, tt.wantPrefix)
		if len(digits) != 3 {
			t.Errorf("code for %q = %q, want exactly 3 digits after the prefix", tt.name, code)
		}
		for _, r := range digits {
			if r < '0' || r > '9' {
				t.Errorf("code for %q = %q, want digits only after the prefix", tt.name, code)
				break
			}
		}
	}
}

func TestGetOrCreatePortalCode_StableAcrossCalls(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	c := &model.Contact{Name: "Andi", ContactType: "customer", Phone: "081111111111", IsActive: true}
	if err := testutil.CreateContact(db, c); err != nil {
		t.Fatalf("create contact: %v", err)
	}
	contacts, _ := testutil.ListContacts(db, testutil.ContactFilter{Search: "Andi"})
	id := contacts[0].ID

	code1, err := contact.New(db).EnsurePortalCode(context.Background(), portalIssuer, id)
	if err != nil {
		t.Fatalf("get code: %v", err)
	}
	if code1 == "" {
		t.Fatal("expected non-empty code")
	}
	code2, err := contact.New(db).EnsurePortalCode(context.Background(), portalIssuer, id)
	if err != nil {
		t.Fatalf("get code again: %v", err)
	}
	if code1 != code2 {
		t.Errorf("code changed across calls: %q != %q", code1, code2)
	}
}

func TestGetOrCreatePortalCode_ConcurrentCallsReturnPersistedWinner(t *testing.T) {
	db := testutil.SetupTestDB(t)
	fixture := &model.Contact{Name: "Concurrent Portal", ContactType: "customer", IsActive: true}
	if err := testutil.CreateContact(db, fixture); err != nil {
		t.Fatal(err)
	}
	contacts, err := testutil.ListContacts(db, testutil.ContactFilter{Search: fixture.Name})
	if err != nil || len(contacts) != 1 {
		t.Fatalf("contact lookup: %v %#v", err, contacts)
	}

	const callers = 20
	codes := make(chan string, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, err := contact.New(db).EnsurePortalCode(context.Background(), portalIssuer, contacts[0].ID)
			if err != nil {
				errs <- err
				return
			}
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	close(errs)
	for err := range errs {
		t.Errorf("GetOrCreatePortalCode: %v", err)
	}
	var persisted string
	if err := db.QueryRow("SELECT portal_code FROM contacts WHERE id=?", contacts[0].ID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	for code := range codes {
		if code != persisted {
			t.Errorf("returned %q, persisted %q", code, persisted)
		}
	}
}

func TestRegeneratePortalCode_InvalidatesOldCode(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	c := &model.Contact{Name: "Budi", ContactType: "customer", Phone: "082222222222", IsActive: true}
	testutil.CreateContact(db, c)
	contacts, _ := testutil.ListContacts(db, testutil.ContactFilter{Search: "Budi"})
	id := contacts[0].ID

	oldCode, _ := contact.New(db).EnsurePortalCode(context.Background(), portalIssuer, id)
	newCode, err := contact.New(db).SetPortalCode(context.Background(), portalManager, id, "")
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if newCode == oldCode {
		t.Fatal("expected a different code after regenerate")
	}

	fam, err := contact.New(db).FamilyByPortalCode(context.Background(), oldCode)
	if err != nil {
		t.Fatalf("lookup old code: %v", err)
	}
	if fam != nil {
		t.Error("old code should no longer resolve to a family")
	}
}

// The point of the short code: a parent retypes it from memory, so casing
// and the dash must not decide whether they get in.
func TestContactsByPortalCode_ResolvesHandTypedCode(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	shared := "083333333333"
	testutil.CreateContact(db, &model.Contact{Name: "Andi", ContactType: "customer", Phone: shared, IsActive: true})
	testutil.CreateContact(db, &model.Contact{Name: "Bayu", ContactType: "customer", Phone: shared, IsActive: true})
	contacts, _ := testutil.ListContacts(db, testutil.ContactFilter{Search: "Andi"})

	code, _ := contact.New(db).EnsurePortalCode(context.Background(), portalIssuer, contacts[0].ID)

	for _, typed := range []string{code, strings.ToLower(code), strings.ReplaceAll(code, "-", ""), strings.ReplaceAll(code, "-", " ")} {
		fam, err := contact.New(db).FamilyByPortalCode(context.Background(), typed)
		if err != nil {
			t.Fatalf("lookup by %q: %v", typed, err)
		}
		if fam == nil || len(fam.Contacts) != 2 {
			t.Fatalf("code %q should resolve the family of 2, got %+v", typed, fam)
		}
		if fam.Code != code {
			t.Errorf("lookup by %q: family Code = %q, want %q", typed, fam.Code, code)
		}
	}
}

// Dangerous edge of matching a nullable column: an empty or punctuation-only
// key must not reach a contact that never got a code.
func TestContactsByPortalCode_BlankCodeDoesNotMatchAll(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	testutil.CreateContact(db, &model.Contact{Name: "No Code", ContactType: "customer", Phone: "084444444444", IsActive: true})

	for _, key := range []string{"", "-", "   ", "--"} {
		fam, err := contact.New(db).FamilyByPortalCode(context.Background(), key)
		if err != nil {
			t.Fatalf("lookup %q: %v", key, err)
		}
		if fam != nil {
			t.Errorf("key %q should not resolve to any family, got %+v", key, fam)
		}
	}
}

func TestNormalizePortalCode(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"andi-829", "andi829"},
		{"ANDI-829", "andi829"},
		{"Andi 829", "andi829"},
		{"ANDI829", "andi829"},
		{"", ""},
		{"-", ""},
	}
	for _, tt := range tests {
		if got := contact.NormalizePortalCode(tt.in); got != tt.want {
			t.Errorf("NormalizePortalCode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSetPortalCode_StoresAndResolves(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	testutil.CreateContact(db, &model.Contact{Name: "Andi", ContactType: "customer", Phone: "081111111111", IsActive: true})
	contacts, _ := testutil.ListContacts(db, testutil.ContactFilter{Search: "Andi"})
	id := contacts[0].ID

	got, err := contact.New(db).SetPortalCode(context.Background(), portalManager, id, "  Andi-Kelas1A  ")
	if err != nil {
		t.Fatalf("set code: %v", err)
	}
	if got != "andi-kelas1a" {
		t.Errorf("code = %q, want it trimmed and lowercased", got)
	}

	fam, err := contact.New(db).FamilyByPortalCode(context.Background(), "ANDIKELAS1A")
	if err != nil || fam == nil {
		t.Fatalf("hand-typed code should resolve, got %+v err=%v", fam, err)
	}
}

// Lookup ignores dashes, so a hand-entered "an-di829" is the same link as an
// existing "andi-829" even though the raw unique index would allow both.
func TestSetPortalCode_RejectsDashOnlyDifference(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	testutil.CreateContact(db, &model.Contact{Name: "One", ContactType: "customer", Phone: "081111111111", IsActive: true})
	testutil.CreateContact(db, &model.Contact{Name: "Two", ContactType: "customer", Phone: "082222222222", IsActive: true})
	contacts, _ := testutil.ListContacts(db, testutil.ContactFilter{})

	if _, err := contact.New(db).SetPortalCode(context.Background(), portalManager, contacts[0].ID, "andi-829"); err != nil {
		t.Fatalf("first code: %v", err)
	}
	for _, clash := range []string{"an-di829", "ANDI-829", "andi829", "a-n-d-i-8-2-9"} {
		if _, err := contact.New(db).SetPortalCode(context.Background(), portalManager, contacts[1].ID, clash); !errors.Is(err, contact.ErrPortalCodeTaken) {
			t.Errorf("%q normalizes onto an existing code, want ErrPortalCodeTaken, got %v", clash, err)
		}
	}
}

func TestSetPortalCode_KeepingOwnCodeIsNotACollision(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	testutil.CreateContact(db, &model.Contact{Name: "Andi", ContactType: "customer", Phone: "081111111111", IsActive: true})
	contacts, _ := testutil.ListContacts(db, testutil.ContactFilter{Search: "Andi"})
	id := contacts[0].ID

	contact.New(db).SetPortalCode(context.Background(), portalManager, id, "andi-829")
	if _, err := contact.New(db).SetPortalCode(context.Background(), portalManager, id, "andi-829"); err != nil {
		t.Errorf("re-saving a contact's own code should be allowed, got %v", err)
	}
}

func TestSetPortalCode_Validation(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	testutil.CreateContact(db, &model.Contact{Name: "Andi", ContactType: "customer", Phone: "081111111111", IsActive: true})
	contacts, _ := testutil.ListContacts(db, testutil.ContactFilter{Search: "Andi"})
	id := contacts[0].ID

	for _, bad := range []string{"and", "a-b", "andi 829", "andi/829", "andi?829", "andi#829", strings.Repeat("a", 33)} {
		if _, err := contact.New(db).SetPortalCode(context.Background(), portalManager, id, bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	// Blank falls back to a generated code rather than erroring.
	got, err := contact.New(db).SetPortalCode(context.Background(), portalManager, id, "   ")
	if err != nil || !strings.HasPrefix(got, "andi-") {
		t.Errorf("blank should generate, got %q err=%v", got, err)
	}
}
