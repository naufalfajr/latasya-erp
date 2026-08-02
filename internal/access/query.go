package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/naufal/latasya-erp/internal/model"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type scanner interface{ Scan(...any) error }

func scanUser(row scanner, includePassword bool) (*model.User, error) {
	u := &model.User{}
	var err error
	if includePassword {
		err = row.Scan(&u.ID, &u.Username, &u.Password, &u.FullName, &u.Role, &u.IsActive, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt)
	} else {
		err = row.Scan(&u.ID, &u.Username, &u.FullName, &u.Role, &u.IsActive, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt)
	}
	return u, err
}

func getUserWith(ctx context.Context, q queryer, column string, value any, includePassword bool) (*model.User, error) {
	columns := "id,username,full_name,role,is_active,must_change_password,created_at,updated_at"
	if includePassword {
		columns = "id,username,password,full_name,role,is_active,must_change_password,created_at,updated_at"
	}
	u, err := scanUser(q.QueryRowContext(ctx, "SELECT "+columns+" FROM users WHERE "+column+"=?", value), includePassword)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

func (m *Module) LookupUserByID(ctx context.Context, id int) (*model.User, error) {
	return getUserWith(ctx, m.db, "id", id, false)
}

func (m *Module) LookupUserForAuth(ctx context.Context, username string) (*model.User, error) {
	return getUserWith(ctx, m.db, "username", username, true)
}

func (m *Module) LookupUserByIDForAuth(ctx context.Context, id int) (*model.User, error) {
	return getUserWith(ctx, m.db, "id", id, true)
}

func (m *Module) GetUser(ctx context.Context, actor Actor, id int) (*model.User, error) {
	if err := require(actor, actor.CanManageUsers); err != nil {
		return nil, err
	}
	return m.LookupUserByID(ctx, id)
}

func (m *Module) ListUsers(ctx context.Context, actor Actor, f ListFilter) (*UserList, error) {
	if err := require(actor, actor.CanManageUsers); err != nil {
		return nil, err
	}
	var total int
	if err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&total); err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}
	query := "SELECT id,username,full_name,role,is_active,must_change_password,created_at,updated_at FROM users ORDER BY id"
	args := []any{}
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, f.Limit, f.Offset)
	}
	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := []model.User{}
	for rows.Next() {
		u, err := scanUser(rows, false)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return &UserList{Users: users, Total: total}, nil
}

func scanRole(row scanner) (*model.Role, error) {
	r := &model.Role{}
	var capabilities string
	if err := row.Scan(&r.Name, &r.Description, &r.IsSystem, &capabilities, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(capabilities), &r.Capabilities); err != nil {
		return nil, fmt.Errorf("decode capabilities for role %q: %w", r.Name, err)
	}
	return r, nil
}

func getRoleWith(ctx context.Context, q queryer, name string) (*model.Role, error) {
	r, err := scanRole(q.QueryRowContext(ctx, "SELECT name,description,is_system,capabilities,created_at,updated_at FROM roles WHERE name=?", name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get role: %w", err)
	}
	return r, nil
}

func (m *Module) LookupRoleForAuth(ctx context.Context, name string) (*model.Role, error) {
	return getRoleWith(ctx, m.db, name)
}

func (m *Module) GetRole(ctx context.Context, actor Actor, name string) (*model.Role, error) {
	if err := require(actor, actor.CanManageUsers || actor.CanManageRoles); err != nil {
		return nil, err
	}
	return m.LookupRoleForAuth(ctx, name)
}

func (m *Module) ListRoles(ctx context.Context, actor Actor, f ListFilter) (*RoleList, error) {
	if err := require(actor, actor.CanManageUsers || actor.CanManageRoles); err != nil {
		return nil, err
	}
	var total int
	if err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM roles").Scan(&total); err != nil {
		return nil, fmt.Errorf("count roles: %w", err)
	}
	query := "SELECT name,description,is_system,capabilities,created_at,updated_at FROM roles ORDER BY is_system DESC,name"
	args := []any{}
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, f.Limit, f.Offset)
	}
	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	roles := []model.Role{}
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roles: %w", err)
	}
	return &RoleList{Roles: roles, Total: total}, nil
}
