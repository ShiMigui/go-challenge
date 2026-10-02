DROP INDEX IF EXISTS wallets_player_id_idx;
DROP TRIGGER IF EXISTS wallets_bump_version_on_balance_change ON wallets;
DROP FUNCTION IF EXISTS wallets_bump_version();
DROP TABLE IF EXISTS wallets;
