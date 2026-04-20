package model

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

type Role struct {
	Name         string
	Description  string
	IsSystem     bool
	Capabilities []string
	CreatedAt    string
	UpdatedAt    string
}

// HasCapability reports whether the role grants the given capability. The
// admin role is treated as having every capability regardless of what is
// stored on the row.
func (r *Role) HasCapability(cap string) bool {
	if r == nil {
		return false
	}
	if r.Name == RoleAdmin {
		return true
	}
	for _, c := range r.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

const roleColumns = "name, description, is_system, capabilities, created_at, updated_at"

func scanRole(row interface{ Scan(...any) error }) (*Role, error) {
	r := &Role{}
	var capsJSON string
	if err := row.Scan(&r.Name, &r.Description, &r.IsSystem, &capsJSON, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(capsJSON), &r.Capabilities); err != nil {
		return nil, fmt.Errorf("decode capabilities for role %q: %w", r.Name, err)
	}
	return r, nil
}

func GetRoleByName(db *sql.DB, name string) (*Role, error) {
	r, err := scanRole(db.QueryRow(
		"SELECT "+roleColumns+" FROM roles WHERE name = ?",
		name,
	))
	if err != nil {
		return nil, fmt.Errorf("get role by name: %w", err)
	}
	return r, nil
}

func ListRoles(db *sql.DB) ([]Role, error) {
	rows, err := db.Query("SELECT " + roleColumns + " FROM roles ORDER BY is_system DESC, name")
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	var roles []Role
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roles: %w", err)
	}
	return roles, nil
}

func CreateRole(db *sql.DB, r *Role) error {
	caps, err := json.Marshal(r.Capabilities)
	if err != nil {
		return fmt.Errorf("encode capabilities: %w", err)
	}
	_, err = db.Exec(
		"INSERT INTO roles (name, description, is_system, capabilities) VALUES (?, ?, ?, ?)",
		r.Name, r.Description, r.IsSystem, string(caps),
	)
	if err != nil {
		return fmt.Errorf("create role: %w", err)
	}
	return nil
}

func UpdateRole(db *sql.DB, r *Role) error {
	caps, err := json.Marshal(r.Capabilities)
	if err != nil {
		return fmt.Errorf("encode capabilities: %w", err)
	}
	_, err = db.Exec(
		"UPDATE roles SET description=?, capabilities=?, updated_at=datetime('now') WHERE name=?",
		r.Description, string(caps), r.Name,
	)
	if err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	return nil
}

func DeleteRole(db *sql.DB, name string) error {
	_, err := db.Exec("DELETE FROM roles WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	return nil
}

// CountUsersWithRole returns how many users currently have the given role
// assigned. Used to block deleting a role that is still in use.
func CountUsersWithRole(db *sql.DB, name string) (int, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE role = ?", name).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count users with role: %w", err)
	}
	return n, nil
}
