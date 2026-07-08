CREATE TABLE IF NOT EXISTS routes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    is_active  INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS vehicles (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    code          TEXT    NOT NULL UNIQUE,
    capacity      INTEGER NOT NULL DEFAULT 0,
    is_active     INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS vehicle_route_assignments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    vehicle_id INTEGER NOT NULL REFERENCES vehicles(id),
    route_id   INTEGER NOT NULL REFERENCES routes(id),
    starts_on  TEXT    NOT NULL,
    ends_on    TEXT,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

ALTER TABLE contacts ADD COLUMN route_id INTEGER REFERENCES routes(id);
ALTER TABLE journal_entries ADD COLUMN vehicle_id INTEGER REFERENCES vehicles(id);

CREATE INDEX IF NOT EXISTS idx_contacts_route_id ON contacts(route_id);
CREATE INDEX IF NOT EXISTS idx_journal_entries_vehicle_id ON journal_entries(vehicle_id);
CREATE INDEX IF NOT EXISTS idx_vehicle_route_current ON vehicle_route_assignments(route_id, ends_on);
CREATE UNIQUE INDEX IF NOT EXISTS idx_vehicle_route_one_active_route ON vehicle_route_assignments(route_id) WHERE ends_on IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_vehicle_route_one_active_vehicle ON vehicle_route_assignments(vehicle_id) WHERE ends_on IS NULL;

INSERT INTO routes (name) VALUES ('West'), ('East');
INSERT INTO vehicles (code, capacity) VALUES ('LA001', 25), ('LA002', 25);
INSERT INTO vehicle_route_assignments (vehicle_id, route_id, starts_on)
SELECT v.id, r.id, date('now')
FROM vehicles v
JOIN routes r ON (v.code = 'LA001' AND r.name = 'West') OR (v.code = 'LA002' AND r.name = 'East');
