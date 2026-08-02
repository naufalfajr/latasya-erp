package model

const (
	SchoolClosureSourceManual = "manual"
	SchoolClosureSourceGoogle = "google"
)

type SchoolClosure struct {
	ID            int    `json:"id"`
	Source        string `json:"source"`
	Title         string `json:"title"`
	StartDate     string `json:"start_date"`
	EndDate       string `json:"end_date"`
	GoogleEventID string `json:"google_event_id,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type GoogleCalendarConnection struct {
	ID             int    `json:"id"`
	CalendarID     string `json:"calendar_id"`
	RefreshToken   string `json:"-"`
	IsActive       bool   `json:"is_active"`
	LastSyncAt     string `json:"last_sync_at"`
	LastSyncStatus string `json:"last_sync_status"`
	LastSyncError  string `json:"last_sync_error"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type GoogleOAuthState struct {
	State        string `json:"state"`
	UserID       int    `json:"user_id"`
	PKCEVerifier string `json:"pkce_verifier"`
	ExpiresAt    string `json:"expires_at"`
	CreatedAt    string `json:"created_at"`
}
