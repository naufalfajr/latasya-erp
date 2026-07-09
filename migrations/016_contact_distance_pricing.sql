CREATE TABLE contacts_new (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    name                 TEXT    NOT NULL,
    contact_type         TEXT    NOT NULL CHECK (contact_type IN ('customer', 'supplier', 'both')),
    phone                TEXT,
    email                TEXT,
    address              TEXT,
    notes                TEXT,
    is_active            INTEGER NOT NULL DEFAULT 1,
    created_at           TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at           TEXT    NOT NULL DEFAULT (datetime('now')),
    maps_link            TEXT    NOT NULL DEFAULT '',
    class                TEXT    NOT NULL DEFAULT '',
    distance_km          INTEGER NOT NULL DEFAULT 0,
    has_sibling_discount INTEGER NOT NULL DEFAULT 0,
    is_return_only       INTEGER NOT NULL DEFAULT 0,
    route_id             INTEGER REFERENCES routes(id)
);

INSERT INTO contacts_new (id, name, contact_type, phone, email, address, notes, is_active, created_at, updated_at, maps_link, class, route_id)
SELECT id, name, contact_type, phone, email, address, notes, is_active, created_at, updated_at, maps_link, class, route_id
FROM contacts;

DROP TABLE contacts;
ALTER TABLE contacts_new RENAME TO contacts;

CREATE INDEX IF NOT EXISTS idx_contacts_type_active ON contacts(contact_type, is_active);
CREATE INDEX IF NOT EXISTS idx_contacts_route_id ON contacts(route_id);
