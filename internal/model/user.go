package model

import (
	"database/sql"
	"fmt"
)

type User struct {
	ID        int
	Username  string
	Password  string
	FullName  string
	Role      string
	IsActive  bool
	CreatedAt string
	UpdatedAt string
}

func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

func GetUserByUsername(db *sql.DB, username string) (*User, error) {
	u := &User{}
	err := db.QueryRow(
		"SELECT id, username, password, full_name, role, is_active, created_at, updated_at FROM users WHERE username = ?",
		username,
	).Scan(&u.ID, &u.Username, &u.Password, &u.FullName, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	return u, nil
}

func GetUserByID(db *sql.DB, id int) (*User, error) {
	u := &User{}
	err := db.QueryRow(
		"SELECT id, username, password, full_name, role, is_active, created_at, updated_at FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Username, &u.Password, &u.FullName, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}

func ListUsers(db *sql.DB) ([]User, error) {
	rows, err := db.Query(
		"SELECT id, username, '', full_name, role, is_active, created_at, updated_at FROM users ORDER BY id",
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Password, &u.FullName, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
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
		"INSERT INTO users (username, password, full_name, role, is_active) VALUES (?, ?, ?, ?, ?)",
		u.Username, u.Password, u.FullName, u.Role, u.IsActive,
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
