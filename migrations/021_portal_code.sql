-- Short parent-portal link, e.g. "andi-829". Replaces 019's long
-- portal_token, which is now unread but left in place rather than dropped.
ALTER TABLE contacts ADD COLUMN portal_code TEXT;

CREATE UNIQUE INDEX idx_contacts_portal_code ON contacts(portal_code)
    WHERE portal_code IS NOT NULL AND portal_code <> '';
