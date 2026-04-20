package model

import (
	"database/sql"
	"fmt"
)

type User struct {
	ID                 int
	Username           string
	Password           string
	FullName           string
	Role               string
	IsActive           bool
	MustChangePassword bool
	CreatedAt          string
	UpdatedAt          string

	// Capabilities is populated by the auth middleware on each request from
	// the user's role. Admin users get a nil/empty slice since HasCapability
	// short-circuits on the role name.
	Capabilities []string
}

func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

// HasCapability reports whether the user's role grants the given capability.
// Admin always returns true. Use this from handlers and templates to gate
// feature-level access.
func (u *User) HasCapability(cap string) bool {
	if u == nil {
		return false
	}
	if u.Role == RoleAdmin {
		return true
	}
	for _, c := range u.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

const userColumns = "id, username, password, full_name, role, is_active, must_change_password, created_at, updated_at"

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	err := row.Scan(
		&u.ID, &u.Username, &u.Password, &u.FullName, &u.Role,
		&u.IsActive, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func GetUserByUsername(db *sql.DB, username string) (*User, error) {
	u, err := scanUser(db.QueryRow(
		"SELECT "+userColumns+" FROM users WHERE username = ?",
		username,
	))
	if err != nil {
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	return u, nil
}

func GetUserByID(db *sql.DB, id int) (*User, error) {
	u, err := scanUser(db.QueryRow(
		"SELECT "+userColumns+" FROM users WHERE id = ?",
		id,
	))
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}

func ListUsers(db *sql.DB) ([]User, error) {
	rows, err := db.Query(
		"SELECT id, username, '', full_name, role, is_active, must_change_password, created_at, updated_at FROM users ORDER BY id",
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(
			&u.ID, &u.Username, &u.Password, &u.FullName, &u.Role,
			&u.IsActive, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

func CreateUser(db *sql.DB, u *User) error {
	_, err := db.Exec(
		"INSERT INTO users (username, password, full_name, role, is_active, must_change_password) VALUES (?, ?, ?, ?, ?, ?)",
		u.Username, u.Password, u.FullName, u.Role, u.IsActive, u.MustChangePassword,
	)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func UpdateUser(db *sql.DB, u *User) error {
	_, err := db.Exec(
		"UPDATE users SET full_name=?, role=?, is_active=?, updated_at=datetime('now') WHERE id=?",
		u.FullName, u.Role, u.IsActive, u.ID,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

func UpdateUserPassword(db *sql.DB, id int, hashedPassword string) error {
	_, err := db.Exec(
		"UPDATE users SET password=?, updated_at=datetime('now') WHERE id=?",
		hashedPassword, id,
	)
	if err != nil {
		return fmt.Errorf("update user password: %w", err)
	}
	return nil
}

// SetMustChangePassword toggles the forced-password-change flag for a user.
func SetMustChangePassword(db *sql.DB, id int, must bool) error {
	_, err := db.Exec(
		"UPDATE users SET must_change_password=?, updated_at=datetime('now') WHERE id=?",
		must, id,
	)
	if err != nil {
		return fmt.Errorf("set must_change_password: %w", err)
	}
	return nil
}
