CREATE TABLE document_sequences (
    document_type TEXT NOT NULL,
    period TEXT NOT NULL,
    last_number INTEGER NOT NULL,
    PRIMARY KEY (document_type, period)
);

CREATE UNIQUE INDEX idx_journal_entries_reference_unique
    ON journal_entries(reference)
    WHERE reference IS NOT NULL AND reference <> '';
