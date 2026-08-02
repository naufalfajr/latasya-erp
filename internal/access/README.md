# Access module

This module owns user administration and roles because their invariants cross:
users must reference valid roles, assigned roles cannot be deleted, and an
administrator cannot deactivate their own account.

Authentication middleware uses the read methods. Password verification and
session management remain in `internal/auth`; password hashes cross this
boundary only through the trusted authentication methods.
