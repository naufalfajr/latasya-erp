# Account module

This module owns the chart of accounts, including account validation, duplicate
code conflicts, cash-account rules, deletion protection, and audit events.

HTTP handlers translate requests and module errors only. Other modules may use
the read methods for account selectors; accounting transactions that must
validate an account inside their own database transaction keep that validation
local to the transaction.
