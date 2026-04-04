package database

import (
	"database/sql"
	"fmt"
	"log/slog"

	"golang.org/x/crypto/bcrypt"
)

func Seed(db *sql.DB) error {
	// Check if admin user exists
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'admin'").Scan(&count)
	if err != nil {
		return fmt.Errorf("check admin user: %w", err)
	}
	if count > 0 {
		return nil
	}

	slog.Info("seeding default admin user")

	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	_, err = db.Exec(
		"INSERT INTO users (username, password, full_name, role) VALUES (?, ?, ?, ?)",
		"admin", string(hash), "Administrator", "admin",
	)
	if err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}

	return nil
}
