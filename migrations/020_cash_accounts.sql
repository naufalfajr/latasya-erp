ALTER TABLE accounts ADD COLUMN is_cash INTEGER NOT NULL DEFAULT 0 CHECK (is_cash IN (0, 1));

UPDATE accounts
SET is_cash = 1
WHERE code IN ('1-1001', '1-1002', '1-1003');

CREATE TRIGGER accounts_cash_insert_guard
BEFORE INSERT ON accounts
WHEN NEW.is_cash = 1 AND (NEW.account_type != 'asset' OR NEW.normal_balance != 'debit')
BEGIN
    SELECT RAISE(ABORT, 'cash accounts must be debit-normal assets');
END;

CREATE TRIGGER accounts_cash_update_guard
BEFORE UPDATE OF is_cash, account_type, normal_balance ON accounts
WHEN NEW.is_cash = 1 AND (NEW.account_type != 'asset' OR NEW.normal_balance != 'debit')
BEGIN
    SELECT RAISE(ABORT, 'cash accounts must be debit-normal assets');
END;
