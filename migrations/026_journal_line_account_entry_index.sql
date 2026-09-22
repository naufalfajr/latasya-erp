-- The journal account filter searches journal lines by exact account and then
-- returns their entry IDs. This composite index covers that EXISTS lookup.
DROP INDEX IF EXISTS idx_jl_account_id;
CREATE INDEX IF NOT EXISTS idx_jl_account_entry ON journal_lines(account_id, entry_id);
