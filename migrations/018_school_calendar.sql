CREATE TABLE IF NOT EXISTS school_closures (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    source          TEXT    NOT NULL CHECK (source IN ('manual', 'google')),
    title           TEXT    NOT NULL,
    start_date      TEXT    NOT NULL,
    end_date        TEXT    NOT NULL,
    google_event_id TEXT,
    created_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    CHECK (start_date <= end_date)
);

CREATE INDEX IF NOT EXISTS idx_school_closures_dates ON school_closures(start_date, end_date);
CREATE UNIQUE INDEX IF NOT EXISTS idx_school_closures_google_event ON school_closures(google_event_id)
    WHERE google_event_id IS NOT NULL AND google_event_id <> '';

CREATE TABLE IF NOT EXISTS google_calendar_connections (
    id               INTEGER PRIMARY KEY CHECK (id = 1),
    calendar_id      TEXT    NOT NULL DEFAULT '',
    refresh_token    TEXT    NOT NULL DEFAULT '',
    is_active        INTEGER NOT NULL DEFAULT 0,
    last_sync_at     TEXT,
    last_sync_status TEXT    NOT NULL DEFAULT '',
    last_sync_error  TEXT    NOT NULL DEFAULT '',
    created_at       TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS google_oauth_states (
    state         TEXT PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pkce_verifier TEXT    NOT NULL,
    expires_at    TEXT    NOT NULL,
    created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_google_oauth_states_expires ON google_oauth_states(expires_at);
