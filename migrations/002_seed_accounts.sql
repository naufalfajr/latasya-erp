-- Assets (1-xxxx) — Normal Balance: Debit
INSERT OR IGNORE INTO accounts (code, name, account_type, normal_balance, is_system, description) VALUES
('1-1001', 'Cash on Hand',             'asset', 'debit', 1, 'Physical cash'),
('1-1002', 'Bank Account - BCA',       'asset', 'debit', 1, 'Main business bank account'),
('1-1003', 'Bank Account - Mandiri',   'asset', 'debit', 0, 'Secondary bank account'),
('1-1100', 'Accounts Receivable',      'asset', 'debit', 1, 'Money owed by customers'),
('1-1200', 'Prepaid Insurance',        'asset', 'debit', 0, 'Insurance paid in advance'),
('1-1300', 'Prepaid Expenses',         'asset', 'debit', 0, 'Other prepaid expenses'),
('1-2001', 'Vehicles',                 'asset', 'debit', 1, 'School buses and travel vehicles'),
('1-2002', 'Vehicle Equipment',        'asset', 'debit', 0, 'GPS, cameras, AC units'),
('1-2003', 'Office Equipment',         'asset', 'debit', 0, 'Computers, printers, furniture'),
('1-2900', 'Accumulated Depreciation', 'asset', 'debit', 0, 'Contra-asset for depreciation');

-- Liabilities (2-xxxx) — Normal Balance: Credit
INSERT OR IGNORE INTO accounts (code, name, account_type, normal_balance, is_system, description) VALUES
('2-1001', 'Accounts Payable',    'liability', 'credit', 1, 'Money owed to suppliers'),
('2-1100', 'Accrued Expenses',    'liability', 'credit', 0, 'Expenses incurred but not yet paid'),
('2-1200', 'Tax Payable',         'liability', 'credit', 0, 'Taxes owed to government'),
('2-2001', 'Vehicle Loans',       'liability', 'credit', 0, 'Long-term vehicle financing'),
('2-2002', 'Other Long-term Debt','liability', 'credit', 0, 'Other loans');

-- Equity (3-xxxx) — Normal Balance: Credit
INSERT OR IGNORE INTO accounts (code, name, account_type, normal_balance, is_system, description) VALUES
('3-1001', 'Owner Capital',      'equity', 'credit', 1, 'Owner investment in the business'),
('3-1002', 'Owner Drawings',     'equity', 'credit', 0, 'Money taken out by owner'),
('3-3001', 'Retained Earnings',  'equity', 'credit', 1, 'Accumulated profits from prior periods');

-- Revenue (4-xxxx) — Normal Balance: Credit
INSERT OR IGNORE INTO accounts (code, name, account_type, normal_balance, is_system, description) VALUES
('4-1001', 'Revenue - School Bus Contract',   'revenue', 'credit', 1, 'Monthly school bus service fees'),
('4-1002', 'Revenue - School Bus Extra Trip',  'revenue', 'credit', 0, 'Additional/special school trips'),
('4-2001', 'Revenue - Travel Charter',         'revenue', 'credit', 1, 'Travel/tour charter bookings'),
('4-2002', 'Revenue - Airport Transfer',       'revenue', 'credit', 0, 'Airport pickup/dropoff service'),
('4-9001', 'Other Revenue',                    'revenue', 'credit', 0, 'Miscellaneous income');

-- Expenses (5-xxxx) — Normal Balance: Debit
INSERT OR IGNORE INTO accounts (code, name, account_type, normal_balance, is_system, description) VALUES
('5-1001', 'Fuel - Solar/Diesel',      'expense', 'debit', 1, 'Diesel fuel for vehicles'),
('5-1002', 'Fuel - Pertamax/Gasoline', 'expense', 'debit', 0, 'Gasoline for vehicles'),
('5-2001', 'Vehicle Maintenance',      'expense', 'debit', 1, 'Repairs and regular maintenance'),
('5-2002', 'Vehicle Spare Parts',      'expense', 'debit', 0, 'Replacement parts'),
('5-2003', 'Tire Replacement',         'expense', 'debit', 0, 'Tire purchases and retreading'),
('5-3001', 'Driver Salary',            'expense', 'debit', 1, 'Monthly driver wages'),
('5-3002', 'Helper/Conductor Salary',  'expense', 'debit', 0, 'Bus helper/kenek wages'),
('5-3003', 'Office Staff Salary',      'expense', 'debit', 0, 'Admin and office staff wages'),
('5-3004', 'THR / Bonus',              'expense', 'debit', 0, 'Holiday allowance and bonuses'),
('5-4001', 'Vehicle Insurance',        'expense', 'debit', 1, 'Annual vehicle insurance premiums'),
('5-4002', 'Business Insurance',       'expense', 'debit', 0, 'General business insurance'),
('5-5001', 'Toll Fees',                'expense', 'debit', 1, 'Highway/expressway tolls'),
('5-5002', 'Parking Fees',             'expense', 'debit', 0, 'Parking charges'),
('5-6001', 'Vehicle Tax (PKB/STNK)',   'expense', 'debit', 1, 'Annual vehicle registration tax'),
('5-6002', 'KIR Inspection',           'expense', 'debit', 0, 'Mandatory vehicle inspection fees'),
('5-6003', 'Route Permit',             'expense', 'debit', 0, 'Operating route permits'),
('5-7001', 'Office Rent',              'expense', 'debit', 0, 'Garage/office rental'),
('5-7002', 'Utilities',                'expense', 'debit', 0, 'Electricity, water, internet'),
('5-7003', 'Phone & Communication',    'expense', 'debit', 0, 'Phone bills, SIM cards for drivers'),
('5-7004', 'Office Supplies',          'expense', 'debit', 0, 'Paper, printer ink, etc.'),
('5-8001', 'Depreciation Expense',     'expense', 'debit', 0, 'Monthly vehicle depreciation'),
('5-9001', 'Bank Charges',             'expense', 'debit', 0, 'Bank admin fees, transfer fees'),
('5-9002', 'Miscellaneous Expense',    'expense', 'debit', 0, 'Other/uncategorized expenses');
