package model

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Fits the long first names that occur here ("abdurrahman" is 11).
const portalCodePrefixMax = 12

// portalCodePrefix takes the first name, ASCII letters only, lowercased.
// Falls back to "lts" when that leaves nothing usable.
func portalCodePrefix(name string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(name), " ")

	var b strings.Builder
	for _, r := range strings.ToLower(first) {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
		if b.Len() == portalCodePrefixMax {
			break
		}
	}
	if b.Len() == 0 {
		return "lts"
	}
	return b.String()
}

// GetOrCreatePortalCode returns the contact's short portal code, generating
// and persisting one on first use.
func GetOrCreatePortalCode(db *sql.DB, contactID int) (string, error) {
	return GetOrCreatePortalCodeContext(context.Background(), db, contactID)
}

func GetOrCreatePortalCodeContext(ctx context.Context, db *sql.DB, contactID int) (string, error) {
	var code string
	err := db.QueryRowContext(ctx, "SELECT COALESCE(portal_code,'') FROM contacts WHERE id = ?", contactID).Scan(&code)
	if err != nil {
		return "", fmt.Errorf("get portal code: %w", err)
	}
	if code != "" {
		return code, nil
	}
	return savePortalCodeContext(ctx, db, contactID, true)
}

// RegeneratePortalCode assigns a fresh code ("andi-829"), invalidating any
// link issued before. Only the 3 digits are secret, so /p/ is rate limited.
func RegeneratePortalCode(db *sql.DB, contactID int) (string, error) {
	return savePortalCodeContext(context.Background(), db, contactID, false)
}

func savePortalCodeContext(ctx context.Context, db *sql.DB, contactID int, onlyIfEmpty bool) (string, error) {
	var name string
	if err := db.QueryRowContext(ctx, "SELECT name FROM contacts WHERE id = ?", contactID).Scan(&name); err != nil {
		return "", fmt.Errorf("get contact name: %w", err)
	}
	prefix := portalCodePrefix(name)

	// ponytail: retry on conflict beats a SELECT on every generation.
	for range 10 {
		n, err := rand.Int(rand.Reader, big.NewInt(1000))
		if err != nil {
			return "", fmt.Errorf("generate portal code: %w", err)
		}
		code := fmt.Sprintf("%s-%03d", prefix, n.Int64())
		query := "UPDATE contacts SET portal_code = ? WHERE id = ?"
		if onlyIfEmpty {
			query += " AND (portal_code IS NULL OR portal_code = '')"
		}
		result, err := db.ExecContext(ctx, query, code, contactID)
		if err == nil {
			if onlyIfEmpty {
				updated, rowsErr := result.RowsAffected()
				if rowsErr != nil {
					return "", fmt.Errorf("save portal code rows: %w", rowsErr)
				}
				if updated == 0 {
					if scanErr := db.QueryRowContext(ctx, "SELECT COALESCE(portal_code,'') FROM contacts WHERE id=?", contactID).Scan(&code); scanErr != nil {
						return "", fmt.Errorf("load saved portal code: %w", scanErr)
					}
				}
			}
			return code, nil
		}
		if !strings.Contains(err.Error(), "UNIQUE") {
			return "", fmt.Errorf("save portal code: %w", err)
		}
	}
	return "", fmt.Errorf("save portal code: too many collisions for prefix %q", prefix)
}

const portalCodeMinLen, portalCodeMaxLen = 4, 32

// ErrPortalCodeTaken is returned when a hand-entered code would collide with
// another contact's, ignoring dashes and case the way lookup does.
var ErrPortalCodeTaken = errors.New("portal code already used by another contact")

// SetPortalCode stores a staff-chosen code, lowercased and validated. Passing
// a blank code falls back to generating a random one.
func SetPortalCode(db *sql.DB, contactID int, code string) (string, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if NormalizePortalCode(code) == "" {
		return RegeneratePortalCode(db, contactID)
	}

	for _, r := range code {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return "", fmt.Errorf("portal code may only contain letters, numbers and dashes")
		}
	}
	if n := len(NormalizePortalCode(code)); n < portalCodeMinLen || n > portalCodeMaxLen {
		return "", fmt.Errorf("portal code must be %d-%d characters", portalCodeMinLen, portalCodeMaxLen)
	}

	// Uniqueness is checked on the normalized form, not the raw column: lookup
	// ignores dashes, so "an-di829" and "andi-829" would be the same link.
	var other int
	err := db.QueryRow(
		`SELECT id FROM contacts
		 WHERE id <> ? AND portal_code IS NOT NULL AND portal_code <> ''
		   AND LOWER(REPLACE(portal_code, '-', '')) = ?`,
		contactID, NormalizePortalCode(code),
	).Scan(&other)
	if err == nil {
		return "", ErrPortalCodeTaken
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("check portal code: %w", err)
	}

	if _, err := db.Exec("UPDATE contacts SET portal_code = ? WHERE id = ?", code, contactID); err != nil {
		return "", fmt.Errorf("save portal code: %w", err)
	}
	return code, nil
}

var portalCodeCleaner = strings.NewReplacer("-", "", " ", "")

// NormalizePortalCode makes a hand-typed code comparable: "ANDI829",
// "andi-829" and "Andi 829" all resolve to the same contact.
func NormalizePortalCode(code string) string {
	return portalCodeCleaner.Replace(strings.ToLower(code))
}

// PortalFamily is the set of contacts reachable from one parent portal
// code: the code's own contact plus any siblings sharing its phone number.
type PortalFamily struct {
	Contacts []Contact
	// Origin contact's code, echoed back so a parent knows what to save.
	Code string
}

// ContactIDs returns the family's contact IDs.
func (f *PortalFamily) ContactIDs() []int {
	ids := make([]int, len(f.Contacts))
	for i, c := range f.Contacts {
		ids[i] = c.ID
	}
	return ids
}

// Has reports whether contactID belongs to this family.
func (f *PortalFamily) Has(contactID int) bool {
	for _, c := range f.Contacts {
		if c.ID == contactID {
			return true
		}
	}
	return false
}

// ContactsByPortalCode resolves a code to its contact plus siblings sharing
// its phone. Blank phones never group, so they can't leak across families.
func ContactsByPortalCode(db *sql.DB, code string) (*PortalFamily, error) {
	normalized := NormalizePortalCode(code)
	if normalized == "" {
		return nil, nil
	}

	var origin Contact
	// ponytail: REPLACE() defeats the index, so this scans. Fine at this scale.
	err := db.QueryRow(
		`SELECT id, name, COALESCE(phone,''), COALESCE(portal_code,'') FROM contacts
		 WHERE portal_code IS NOT NULL AND portal_code <> ''
		   AND LOWER(REPLACE(portal_code, '-', '')) = ?`,
		normalized,
	).Scan(&origin.ID, &origin.Name, &origin.Phone, &origin.PortalCode)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup portal code: %w", err)
	}

	if origin.Phone == "" {
		return &PortalFamily{Contacts: []Contact{origin}, Code: origin.PortalCode}, nil
	}
	originDigits := NormalizePhoneID(origin.Phone)

	// ponytail: normalization can't be expressed in the WHERE clause, so this
	// scans every contact with a phone on file and filters in Go. Fine at
	// this business's contact-book scale; move to a stored normalized-phone
	// column if that ever changes.
	rows, err := db.Query("SELECT id, name, phone FROM contacts WHERE phone IS NOT NULL AND phone <> '' ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list family contacts: %w", err)
	}
	defer rows.Close()

	var family []Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone); err != nil {
			return nil, fmt.Errorf("scan family contact: %w", err)
		}
		if NormalizePhoneID(c.Phone) == originDigits {
			family = append(family, Contact{ID: c.ID, Name: c.Name})
		}
	}
	return &PortalFamily{Contacts: family, Code: origin.PortalCode}, nil
}
