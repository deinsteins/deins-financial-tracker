DROP INDEX IF EXISTS uidx_net_worth_snapshots_user_date;
DROP INDEX IF EXISTS idx_liabilities_liability_type;
DROP INDEX IF EXISTS idx_assets_asset_type;
DROP INDEX IF EXISTS idx_net_worth_snapshots_user_id;
DROP INDEX IF EXISTS idx_liabilities_user_id;
DROP INDEX IF EXISTS idx_assets_user_id;

DROP TABLE IF EXISTS net_worth_snapshots;
DROP TABLE IF EXISTS liabilities;
DROP TABLE IF EXISTS assets;
