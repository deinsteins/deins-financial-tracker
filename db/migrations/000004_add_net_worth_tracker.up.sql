CREATE TABLE IF NOT EXISTS assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    asset_type VARCHAR(50) NOT NULL,
    name VARCHAR(100) NOT NULL,
    amount BIGINT NOT NULL,
    currency VARCHAR(10) NOT NULL DEFAULT 'IDR',
    notes TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS liabilities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    liability_type VARCHAR(50) NOT NULL,
    name VARCHAR(100) NOT NULL,
    amount BIGINT NOT NULL,
    currency VARCHAR(10) NOT NULL DEFAULT 'IDR',
    due_date DATE,
    notes TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS net_worth_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    total_assets BIGINT NOT NULL,
    total_liabilities BIGINT NOT NULL,
    net_worth BIGINT NOT NULL,
    snapshot_date DATE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Add indexes for user_id
CREATE INDEX IF NOT EXISTS idx_assets_user_id ON assets(user_id);
CREATE INDEX IF NOT EXISTS idx_liabilities_user_id ON liabilities(user_id);
CREATE INDEX IF NOT EXISTS idx_net_worth_snapshots_user_id ON net_worth_snapshots(user_id);

-- Add indexes for asset_type and liability_type
CREATE INDEX IF NOT EXISTS idx_assets_asset_type ON assets(asset_type);
CREATE INDEX IF NOT EXISTS idx_liabilities_liability_type ON liabilities(liability_type);

-- Add unique index for user_id + snapshot_date in net_worth_snapshots
CREATE UNIQUE INDEX IF NOT EXISTS uidx_net_worth_snapshots_user_date ON net_worth_snapshots(user_id, snapshot_date);
