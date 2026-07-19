-- portal_token grants a parent (contact) durable, unauthenticated access to
-- their own family's invoices at /i/{token}. Generated lazily on first use,
-- not backfilled here.
ALTER TABLE contacts ADD COLUMN portal_token TEXT;

CREATE UNIQUE INDEX idx_contacts_portal_token ON contacts(portal_token)
    WHERE portal_token IS NOT NULL AND portal_token <> '';
