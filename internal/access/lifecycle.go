package access

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func validateUser(d UserDraft, create bool) error {
	fields := map[string]string{}
	if create && strings.TrimSpace(d.Username) == "" {
		fields["username"] = "required"
	}
	if strings.TrimSpace(d.FullName) == "" {
		fields["full_name"] = "required"
	}
	if d.Role == "" {
		fields["role"] = "required"
	}
	if create && d.Password == "" {
		fields["password"] = "required"
	} else if d.Password != "" && len(d.Password) < 4 {
		fields["password"] = "minimum 4 characters"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func (m *Module) CreateUser(ctx context.Context, actor Actor, d UserDraft) (*model.User, error) {
	if err := require(actor, actor.CanManageUsers); err != nil {
		return nil, err
	}
	if err := validateUser(d, true); err != nil {
		return nil, err
	}
	if m.hash == nil {
		return nil, fmt.Errorf("create user: password hasher unavailable")
	}
	hash, err := m.hash(d.Password)
	if err != nil {
		return nil, fmt.Errorf("hash user password: %w", err)
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin user create: %w", err)
	}
	defer tx.Rollback()
	if _, err := getRoleWith(ctx, tx, d.Role); err != nil {
		if err == ErrNotFound {
			return nil, &ValidationError{Fields: map[string]string{"role": "invalid role"}}
		}
		return nil, err
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO users (username,password,full_name,role,is_active,must_change_password) VALUES (?,?,?,?,?,1)", strings.TrimSpace(d.Username), hash, strings.TrimSpace(d.FullName), d.Role, d.IsActive)
	if err != nil {
		if isUnique(err) {
			return nil, &ConflictError{Message: "username already exists"}
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	id64, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("user id: %w", err)
	}
	created, err := getUserWith(ctx, tx, "id", int(id64), false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit user create: %w", err)
	}
	m.auditUser(ctx, actor, "user.create", created, map[string]any{"after": userSnapshot(created)})
	return created, nil
}

func (m *Module) UpdateUser(ctx context.Context, actor Actor, id int, d UserDraft) (*model.User, error) {
	if err := require(actor, actor.CanManageUsers); err != nil {
		return nil, err
	}
	if err := validateUser(d, false); err != nil {
		return nil, err
	}
	if actor.UserID == id && !d.IsActive {
		return nil, &ConflictError{Message: "cannot deactivate your own account"}
	}
	var passwordHash string
	if d.Password != "" {
		if m.hash == nil {
			return nil, fmt.Errorf("update user: password hasher unavailable")
		}
		var err error
		passwordHash, err = m.hash(d.Password)
		if err != nil {
			return nil, fmt.Errorf("hash user password: %w", err)
		}
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin user update: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE users SET id=id WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("lock user: %w", err)
	}
	old, err := getUserWith(ctx, tx, "id", id, false)
	if err != nil {
		return nil, err
	}
	if _, err := getRoleWith(ctx, tx, d.Role); err != nil {
		if err == ErrNotFound {
			return nil, &ValidationError{Fields: map[string]string{"role": "invalid role"}}
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET full_name=?,role=?,is_active=?,updated_at=datetime('now') WHERE id=?", strings.TrimSpace(d.FullName), d.Role, d.IsActive, id); err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	if passwordHash != "" {
		mustChange := actor.UserID != id
		if _, err := tx.ExecContext(ctx, "UPDATE users SET password=?,must_change_password=?,updated_at=datetime('now') WHERE id=?", passwordHash, mustChange, id); err != nil {
			return nil, fmt.Errorf("update user password: %w", err)
		}
	}
	updated, err := getUserWith(ctx, tx, "id", id, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit user update: %w", err)
	}
	metadata := audit.Diff(userSnapshot(old), userSnapshot(updated), []string{"full_name", "role", "is_active"})
	if d.Password != "" {
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["password_reset"] = true
	}
	if metadata != nil {
		m.auditUser(ctx, actor, "user.update", old, metadata)
	}
	return updated, nil
}

func (m *Module) DeactivateUser(ctx context.Context, actor Actor, id int) (*model.User, error) {
	if err := require(actor, actor.CanManageUsers); err != nil {
		return nil, err
	}
	if actor.UserID == id {
		return nil, &ConflictError{Message: "cannot deactivate your own account"}
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin user deactivate: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE users SET id=id WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("lock user: %w", err)
	}
	old, err := getUserWith(ctx, tx, "id", id, false)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET is_active=0,updated_at=datetime('now') WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("deactivate user: %w", err)
	}
	updated, err := getUserWith(ctx, tx, "id", id, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit user deactivate: %w", err)
	}
	m.auditUser(ctx, actor, "user.delete", old, map[string]any{"before": map[string]any{"is_active": old.IsActive}, "after": map[string]any{"is_active": false}})
	return updated, nil
}

// StorePasswordHash is for trusted authentication flows after password verification.
func (m *Module) StorePasswordHash(ctx context.Context, id int, hash string, mustChange bool) error {
	result, err := m.db.ExecContext(ctx, "UPDATE users SET password=?,must_change_password=?,updated_at=datetime('now') WHERE id=?", hash, mustChange, id)
	if err != nil {
		return fmt.Errorf("store password hash: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (m *Module) SetPasswordChangeRequired(ctx context.Context, id int, required bool) error {
	result, err := m.db.ExecContext(ctx, "UPDATE users SET must_change_password=?,updated_at=datetime('now') WHERE id=?", required, id)
	if err != nil {
		return fmt.Errorf("set password change required: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func validateRole(d RoleDraft, create bool) error {
	fields := map[string]string{}
	if create {
		if d.Name == "" {
			fields["name"] = "required"
		} else if !roleNamePattern.MatchString(d.Name) {
			fields["name"] = "use lowercase letters, digits, hyphens or underscores (must start with a letter)"
		} else if d.Name == model.RoleAdmin {
			fields["name"] = "reserved role name"
		}
	}
	allowed := map[string]bool{}
	for _, capability := range model.AllCapabilities {
		allowed[capability] = true
	}
	for _, capability := range d.Capabilities {
		if !allowed[capability] {
			fields["capabilities"] = "unknown capability: " + capability
			break
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func (m *Module) CreateRole(ctx context.Context, actor Actor, d RoleDraft) (*model.Role, error) {
	if err := require(actor, actor.CanManageRoles); err != nil {
		return nil, err
	}
	d.Name = strings.TrimSpace(d.Name)
	if err := validateRole(d, true); err != nil {
		return nil, err
	}
	capabilities, err := json.Marshal(nonNil(d.Capabilities))
	if err != nil {
		return nil, fmt.Errorf("encode role capabilities: %w", err)
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin role create: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO roles (name,description,is_system,capabilities) VALUES (?,?,0,?)", d.Name, strings.TrimSpace(d.Description), string(capabilities))
	if err != nil {
		if isUnique(err) {
			return nil, &ConflictError{Message: "role name already exists"}
		}
		return nil, fmt.Errorf("create role: %w", err)
	}
	created, err := getRoleWith(ctx, tx, d.Name)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit role create: %w", err)
	}
	m.auditRole(ctx, actor, "role.create", created, map[string]any{"after": roleSnapshot(created)})
	return created, nil
}

func (m *Module) UpdateRole(ctx context.Context, actor Actor, name string, d RoleDraft) (*model.Role, error) {
	if err := require(actor, actor.CanManageRoles); err != nil {
		return nil, err
	}
	if err := validateRole(d, false); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin role update: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE roles SET name=name WHERE name=?", name); err != nil {
		return nil, fmt.Errorf("lock role: %w", err)
	}
	old, err := getRoleWith(ctx, tx, name)
	if err != nil {
		return nil, err
	}
	if old.Name == model.RoleAdmin {
		return nil, &ConflictError{Message: "the admin role cannot be edited"}
	}
	capabilities, err := json.Marshal(nonNil(d.Capabilities))
	if err != nil {
		return nil, fmt.Errorf("encode role capabilities: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE roles SET description=?,capabilities=?,updated_at=datetime('now') WHERE name=?", strings.TrimSpace(d.Description), string(capabilities), name); err != nil {
		return nil, fmt.Errorf("update role: %w", err)
	}
	updated, err := getRoleWith(ctx, tx, name)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit role update: %w", err)
	}
	oldCaps, newCaps := sorted(old.Capabilities), sorted(updated.Capabilities)
	if metadata := audit.Diff(map[string]any{"description": old.Description, "capabilities": oldCaps}, map[string]any{"description": updated.Description, "capabilities": newCaps}, []string{"description", "capabilities"}); metadata != nil {
		m.auditRole(ctx, actor, "role.update", old, metadata)
	}
	return updated, nil
}

func (m *Module) DeleteRole(ctx context.Context, actor Actor, name string) (*model.Role, error) {
	if err := require(actor, actor.CanManageRoles); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin role delete: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE roles SET name=name WHERE name=?", name); err != nil {
		return nil, fmt.Errorf("lock role: %w", err)
	}
	role, err := getRoleWith(ctx, tx, name)
	if err != nil {
		return nil, err
	}
	if role.IsSystem {
		return nil, &ConflictError{Message: "system roles cannot be deleted"}
	}
	var users int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role=?", name).Scan(&users); err != nil {
		return nil, fmt.Errorf("count role users: %w", err)
	}
	if users > 0 {
		return nil, &ConflictError{Message: "cannot delete role: still assigned to users"}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM roles WHERE name=?", name); err != nil {
		return nil, fmt.Errorf("delete role: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit role delete: %w", err)
	}
	m.auditRole(ctx, actor, "role.delete", role, map[string]any{"before": roleSnapshot(role)})
	return role, nil
}

func userSnapshot(u *model.User) map[string]any {
	return map[string]any{"username": u.Username, "full_name": u.FullName, "role": u.Role, "is_active": u.IsActive}
}

func roleSnapshot(r *model.Role) map[string]any {
	return map[string]any{"name": r.Name, "description": r.Description, "capabilities": r.Capabilities}
}

func (m *Module) auditUser(ctx context.Context, actor Actor, action string, user *model.User, metadata map[string]any) {
	audit.Log(ctx, m.db, audit.Event{Action: action, TargetType: "user", TargetID: int64(user.ID), TargetLabel: user.Username, Metadata: metadata, ActorID: int64(actor.UserID)})
}

func (m *Module) auditRole(ctx context.Context, actor Actor, action string, role *model.Role, metadata map[string]any) {
	audit.Log(ctx, m.db, audit.Event{Action: action, TargetType: "role", TargetLabel: role.Name, Metadata: metadata, ActorID: int64(actor.UserID)})
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func sorted(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func isUnique(err error) bool { return strings.Contains(strings.ToLower(err.Error()), "unique") }
