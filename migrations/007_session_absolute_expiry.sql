-- Sliding sessions: expires_at is now the idle deadline and slides forward
-- on activity. absolute_expires_at is a hard cap that TouchSession cannot
-- push past, so an actively-used session still dies after the absolute max.
ALTER TABLE sessions ADD COLUMN absolute_expires_at TEXT NOT NULL DEFAULT '';

-- Backfill existing rows with their current expires_at as the absolute cap
-- so they expire as originally scheduled — no surprise extensions, no
-- surprise early-logouts on deploy.
UPDATE sessions SET absolute_expires_at = expires_at WHERE absolute_expires_at = '';
