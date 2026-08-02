CREATE TABLE document_sequences (
    document_type TEXT NOT NULL,
    period TEXT NOT NULL,
    last_number INTEGER NOT NULL,
    PRIMARY KEY (document_type, period)
);
