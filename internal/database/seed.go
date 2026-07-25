package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"

	"github.com/naufal/latasya-erp/internal/auth"
)

// Production seeds one database per process, so hashing once costs nothing
// there; tests seed hundreds and this is 83% of their setup time. auth's cost
// drops to bcrypt.MinCost under test, which also makes every admin login in
// the suite cheap to verify.
var defaultAdminHash = sync.OnceValues(func() (string, error) {
	return auth.HashPassword("admin")
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
		"admin", hash, "Administrator", "admin",
	)
	if err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}

	return nil
}
