package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// Production seeds one database per process, so hashing once costs nothing
// there; tests seed hundreds and this is 83% of their setup time.
var defaultAdminHash = sync.OnceValues(func() ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
})

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

	slog.Info("seeding default admin user (password change required on first login)")

	hash, err := defaultAdminHash()
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	_, err = db.Exec(
		"INSERT INTO users (username, password, full_name, role, must_change_password) VALUES (?, ?, ?, ?, 1)",
		"admin", string(hash), "Administrator", "admin",
	)
	if err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}

	return nil
}
