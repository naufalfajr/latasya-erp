package access

import "github.com/naufal/latasya-erp/internal/model"

type ListFilter struct {
	Limit  int
	Offset int
}

type UserList struct {
	Users []model.User
	Total int
}

type RoleList struct {
	Roles []model.Role
	Total int
}

type UserDraft struct {
	Username string
	FullName string
	Role     string
	IsActive bool
	Password string
}

type RoleDraft struct {
	Name         string
	Description  string
	Capabilities []string
}
