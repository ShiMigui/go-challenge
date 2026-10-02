DROP TRIGGER IF EXISTS wallet_ledger_entries_no_truncate ON wallet_ledger_entries;
DROP TRIGGER IF EXISTS wallet_ledger_entries_no_delete ON wallet_ledger_entries;
DROP TRIGGER IF EXISTS wallet_ledger_entries_no_update ON wallet_ledger_entries;
DROP FUNCTION IF EXISTS wallet_ledger_entries_immutable();
DROP TABLE IF EXISTS wallet_ledger_entries;
DROP TYPE IF EXISTS ledger_direction;