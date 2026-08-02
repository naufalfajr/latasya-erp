package model

import "time"

type APIToken struct {
	ID          int
	UserID      int
	Name        string
	TokenPrefix string
	Scopes      []string
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}
