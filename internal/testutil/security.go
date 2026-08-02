package testutil

import (
	"context"
	"database/sql"
	"time"

	"github.com/naufal/latasya-erp/internal/access"
	"github.com/naufal/latasya-erp/internal/apitoken"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/schoolcalendar"
)

func GetUserByID(db *sql.DB, id int) (*model.User, error) {
	return access.New(db, nil).LookupUserByIDForAuth(context.Background(), id)
}
func GetUserByUsername(db *sql.DB, username string) (*model.User, error) {
	return access.New(db, nil).LookupUserForAuth(context.Background(), username)
}
func SetMustChangePassword(db *sql.DB, id int, required bool) error {
	return access.New(db, nil).SetPasswordChangeRequired(context.Background(), id, required)
}
func ListRoles(db *sql.DB) ([]model.Role, error) {
	result, err := access.New(db, nil).ListRoles(context.Background(), access.Actor{UserID: 1, CanManageRoles: true}, access.ListFilter{})
	if err != nil {
		return nil, err
	}
	return result.Roles, nil
}
func GetRoleByName(db *sql.DB, name string) (*model.Role, error) {
	return access.New(db, nil).LookupRoleForAuth(context.Background(), name)
}
func CreateRole(db *sql.DB, role *model.Role) error {
	_, err := access.New(db, auth.HashPassword).CreateRole(context.Background(), access.Actor{UserID: 1, CanManageRoles: true}, access.RoleDraft{Name: role.Name, Description: role.Description, Capabilities: role.Capabilities})
	return err
}

func CreateAPIToken(db *sql.DB, userID int, name string, scopes []string, expiresAt *time.Time) (*model.APIToken, string, error) {
	draftExpiry := expiresAt
	if expiresAt != nil && !expiresAt.After(time.Now().UTC()) {
		draftExpiry = nil
	}
	created, err := apitoken.New(db).Create(context.Background(), apitoken.Actor{UserID: userID, IsAdmin: true}, apitoken.Draft{Name: name, Scopes: scopes, ExpiresAt: draftExpiry})
	if err != nil {
		return nil, "", err
	}
	if expiresAt != nil && draftExpiry == nil {
		if _, err := db.Exec(`UPDATE api_tokens SET expires_at=? WHERE id=?`, expiresAt.UTC().Format(time.RFC3339), created.Token.ID); err != nil {
			return nil, "", err
		}
		created.Token.ExpiresAt = expiresAt
	}
	return created.Token, created.Plaintext, nil
}
func RevokeAPIToken(db *sql.DB, userID, tokenID int) error {
	_, err := apitoken.New(db).Revoke(context.Background(), apitoken.Actor{UserID: userID, IsAdmin: true}, tokenID)
	return err
}

func ListSchoolClosures(db *sql.DB, month string) ([]model.SchoolClosure, error) {
	return schoolcalendar.New(db).List(context.Background(), month)
}
func GetSchoolClosure(db *sql.DB, id int) (*model.SchoolClosure, error) {
	return schoolcalendar.New(db).Get(context.Background(), id)
}
func CreateSchoolClosure(db *sql.DB, c *model.SchoolClosure) (int, error) {
	created, err := schoolcalendar.New(db).CreateManual(context.Background(), schoolcalendar.Actor{UserID: 1, CanManage: true}, schoolcalendar.ClosureDraft{Title: c.Title, StartDate: c.StartDate, EndDate: c.EndDate})
	if err != nil {
		return 0, err
	}
	*c = *created
	return created.ID, nil
}
func DeleteSchoolClosure(db *sql.DB, id int) error {
	_, err := schoolcalendar.New(db).Delete(context.Background(), schoolcalendar.Actor{UserID: 1, CanManage: true}, id)
	return err
}
func EffectiveSchoolDays(db *sql.DB, month string) (int, error) {
	return schoolcalendar.New(db).EffectiveDays(context.Background(), month)
}
func SaveGoogleCalendarConnection(db *sql.DB, c *model.GoogleCalendarConnection) error {
	module := schoolcalendar.New(db)
	actor := schoolcalendar.Actor{UserID: 1, IsAdmin: true}
	ctx := context.Background()
	if _, err := module.SaveCalendarID(ctx, actor, c.CalendarID); err != nil {
		return err
	}
	if c.RefreshToken != "" && c.IsActive {
		if _, err := module.Connect(ctx, actor, c.RefreshToken); err != nil {
			return err
		}
	}
	if c.LastSyncStatus != "" || c.LastSyncError != "" {
		return module.UpdateSyncStatus(ctx, c.LastSyncStatus, c.LastSyncError)
	}
	return nil
}
func GetGoogleCalendarConnection(db *sql.DB) (*model.GoogleCalendarConnection, error) {
	return schoolcalendar.New(db).Connection(context.Background())
}
func DeleteGoogleCalendarConnection(db *sql.DB) error {
	return schoolcalendar.New(db).Disconnect(context.Background(), schoolcalendar.Actor{UserID: 1, IsAdmin: true})
}
func CreateGoogleOAuthState(db *sql.DB, state string, userID int, verifier, expiresAt string) error {
	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return err
	}
	return schoolcalendar.New(db).CreateOAuthState(context.Background(), schoolcalendar.Actor{UserID: userID, IsAdmin: true}, state, verifier, expiry)
}
func ConsumeGoogleOAuthState(db *sql.DB, state string, userID int) (*model.GoogleOAuthState, error) {
	return schoolcalendar.New(db).ConsumeOAuthState(context.Background(), schoolcalendar.Actor{UserID: userID, IsAdmin: true}, state)
}
func ReplaceGoogleSchoolClosures(db *sql.DB, closures []model.SchoolClosure, start, end string) error {
	return schoolcalendar.New(db).ReplaceGoogleClosures(context.Background(), closures, start, end)
}
