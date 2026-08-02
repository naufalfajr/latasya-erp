package model

type User struct {
	ID                 int      `json:"id"`
	Username           string   `json:"username"`
	Password           string   `json:"-"`
	FullName           string   `json:"full_name"`
	Role               string   `json:"role"`
	IsActive           bool     `json:"is_active"`
	MustChangePassword bool     `json:"must_change_password"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
	Capabilities       []string `json:"capabilities,omitempty"`
}

func (u *User) IsAdmin() bool { return u != nil && u.Role == RoleAdmin }
func (u *User) HasCapability(capability string) bool {
	if u == nil {
		return false
	}
	if u.Role == RoleAdmin {
		return true
	}
	for _, held := range u.Capabilities {
		if held == capability {
			return true
		}
	}
	return false
}
