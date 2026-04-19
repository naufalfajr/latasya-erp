-- Add CSRF token to sessions
ALTER TABLE sessions ADD COLUMN csrf_token TEXT NOT NULL DEFAULT '';
UPDATE sessions SET csrf_token = lower(hex(randomblob(32))) WHERE csrf_token = '';

-- Force existing admin to change password (new admin will also be flagged on seed)
ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0;
UPDATE users SET must_change_password = 1 WHERE username = 'admin';
