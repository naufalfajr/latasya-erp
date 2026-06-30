ALTER TABLE contacts ADD COLUMN maps_link TEXT    NOT NULL DEFAULT '';
ALTER TABLE contacts ADD COLUMN class     TEXT    NOT NULL DEFAULT '';
ALTER TABLE contacts ADD COLUMN price     INTEGER NOT NULL DEFAULT 0;

ALTER TABLE company_profile ADD COLUMN default_revenue_account_id INTEGER REFERENCES accounts(id);
ALTER TABLE company_profile ADD COLUMN recurring_description_template TEXT NOT NULL DEFAULT 'Antar jemput {month} {year}';
UPDATE company_profile SET default_revenue_account_id = (SELECT id FROM accounts WHERE code = '4-1001') WHERE id = 1;
