-- Fix Accumulated Depreciation: contra-asset account should have credit normal balance
UPDATE accounts SET normal_balance = 'credit' WHERE code = '1-2900';
