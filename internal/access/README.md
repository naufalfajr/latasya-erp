# Access module

This module owns user administration and roles because their invariants cross:
users must reference valid roles, assigned roles cannot be deleted, and an
administrator cannot deactivate their own account.

User management cannot escalate privileges: a non-admin with `users.manage` may
only assign roles within their own capabilities, and cannot edit or deactivate
users whose role exceeds them. Only admins grant or manage the `admin` role.
`Actor.CanAssign` holds the rule. Over the API a bearer token never counts as
admin and is limited to its effective scopes. `CheckManageable` applies the same
rule elsewhere, e.g. to revoking another user's API tokens.

Authentication middleware uses the read methods. Password verification and
session management remain in `internal/auth`; password hashes cross this
boundary only through the trusted authentication methods.
