package contact

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

const (
	portalCodePrefixMax = 12
	portalCodeMinLen    = 4
	portalCodeMaxLen    = 32
)

var portalCodeCleaner = strings.NewReplacer("-", "", " ", "")

func NormalizePortalCode(code string) string {
	return portalCodeCleaner.Replace(strings.ToLower(code))
}

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

func (m *Module) GetOrCreatePortalCode(ctx context.Context, contactID int) (string, error) {
	var code string
	err := m.db.QueryRowContext(ctx, "SELECT COALESCE(portal_code,'') FROM contacts WHERE id=?", contactID).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get portal code: %w", err)
	}
	if code != "" {
		return code, nil
	}
	return generatePortalCode(ctx, m.db, contactID, "", true)
}

type portalStore interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func generatePortalCode(ctx context.Context, store portalStore, contactID int, name string, onlyIfEmpty bool) (string, error) {
	if name == "" {
		if err := store.QueryRowContext(ctx, "SELECT name FROM contacts WHERE id=?", contactID).Scan(&name); errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		} else if err != nil {
			return "", fmt.Errorf("get contact name: %w", err)
		}
	}
	prefix := portalCodePrefix(name)
	for range 10 {
		n, err := rand.Int(rand.Reader, big.NewInt(1000))
		if err != nil {
			return "", fmt.Errorf("generate portal code: %w", err)
		}
		code := fmt.Sprintf("%s-%03d", prefix, n.Int64())
		query := "UPDATE contacts SET portal_code=? WHERE id=?"
		if onlyIfEmpty {
			query += " AND (portal_code IS NULL OR portal_code='')"
		}
		result, err := store.ExecContext(ctx, query, code, contactID)
		if err == nil {
			updated, rowsErr := result.RowsAffected()
			if rowsErr != nil {
				return "", fmt.Errorf("save portal code rows: %w", rowsErr)
			}
			if updated == 0 {
				if !onlyIfEmpty {
					return "", ErrNotFound
				}
				if err := store.QueryRowContext(ctx, "SELECT COALESCE(portal_code,'') FROM contacts WHERE id=?", contactID).Scan(&code); errors.Is(err, sql.ErrNoRows) {
					return "", ErrNotFound
				} else if err != nil {
					return "", fmt.Errorf("load saved portal code: %w", err)
				}
			}
			return code, nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "unique") {
			return "", fmt.Errorf("save portal code: %w", err)
		}
	}
	return "", fmt.Errorf("save portal code: too many collisions for prefix %q", prefix)
}

func (m *Module) SetPortalCode(ctx context.Context, actor Actor, contactID int, raw string) (string, error) {
	if actor.UserID <= 0 || !actor.CanManagePortal {
		return "", ErrForbidden
	}
	code := strings.ToLower(strings.TrimSpace(raw))
	if NormalizePortalCode(code) != "" {
		for _, r := range code {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return "", &ValidationError{Fields: map[string]string{"portal_code": "may only contain letters, numbers and dashes"}}
			}
		}
		if n := len(NormalizePortalCode(code)); n < portalCodeMinLen || n > portalCodeMaxLen {
			return "", &ValidationError{Fields: map[string]string{"portal_code": fmt.Sprintf("must be %d-%d characters", portalCodeMinLen, portalCodeMaxLen)}}
		}
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin portal code update: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE contacts SET id=id WHERE id=?", contactID); err != nil {
		return "", fmt.Errorf("lock portal contact: %w", err)
	}
	old, err := getWith(ctx, tx, contactID)
	if err != nil {
		return "", err
	}
	if NormalizePortalCode(code) == "" {
		code, err = generatePortalCode(ctx, tx, contactID, old.Name, false)
		if err != nil {
			return "", err
		}
	} else {
		var other int
		err = tx.QueryRowContext(ctx, `SELECT id FROM contacts WHERE id<>? AND portal_code IS NOT NULL AND portal_code<>'' AND LOWER(REPLACE(portal_code,'-',''))=?`, contactID, NormalizePortalCode(code)).Scan(&other)
		if err == nil {
			return "", ErrPortalCodeTaken
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("check portal code: %w", err)
		}
		result, err := tx.ExecContext(ctx, "UPDATE contacts SET portal_code=? WHERE id=?", code, contactID)
		if err != nil {
			return "", fmt.Errorf("save portal code: %w", err)
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			return "", ErrNotFound
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit portal code update: %w", err)
	}
	audit.Log(ctx, m.db, audit.Event{Action: "contact.portal_token_reset", TargetType: "contact", TargetID: int64(contactID), TargetLabel: old.Name, Metadata: map[string]any{"portal_code_changed": old.PortalCode != code}, ActorID: int64(actor.UserID)})
	return code, nil
}

func (m *Module) FamilyByPortalCode(ctx context.Context, code string) (*PortalFamily, error) {
	normalized := NormalizePortalCode(code)
	if normalized == "" {
		return nil, nil
	}
	var origin model.Contact
	err := m.db.QueryRowContext(ctx, `SELECT id,name,COALESCE(phone,''),COALESCE(portal_code,'') FROM contacts WHERE portal_code IS NOT NULL AND portal_code<>'' AND LOWER(REPLACE(portal_code,'-',''))=?`, normalized).Scan(&origin.ID, &origin.Name, &origin.Phone, &origin.PortalCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup portal code: %w", err)
	}
	if origin.Phone == "" {
		return &PortalFamily{Contacts: []model.Contact{origin}, Code: origin.PortalCode}, nil
	}
	digits := model.NormalizePhoneID(origin.Phone)
	rows, err := m.db.QueryContext(ctx, "SELECT id,name,phone FROM contacts WHERE phone IS NOT NULL AND phone<>'' ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list family contacts: %w", err)
	}
	defer rows.Close()
	family := []model.Contact{}
	for rows.Next() {
		var c model.Contact
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone); err != nil {
			return nil, fmt.Errorf("scan family contact: %w", err)
		}
		if model.NormalizePhoneID(c.Phone) == digits {
			family = append(family, model.Contact{ID: c.ID, Name: c.Name})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate family contacts: %w", err)
	}
	return &PortalFamily{Contacts: family, Code: origin.PortalCode}, nil
}
