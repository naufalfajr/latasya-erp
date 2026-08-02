package model

type Role struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	IsSystem     bool     `json:"is_system"`
	Capabilities []string `json:"capabilities"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

func (r *Role) HasCapability(capability string) bool {
	if r == nil {
		return false
	}
	if r.Name == RoleAdmin {
		return true
	}
	for _, held := range r.Capabilities {
		if held == capability {
			return true
		}
	}
	return false
}
