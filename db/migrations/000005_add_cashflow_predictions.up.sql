CREATE TABLE IF NOT EXISTS cashflow_predictions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    start_date DATE NOT NULL,
    target_date DATE NOT NULL,
    available_balance BIGINT NOT NULL,
    daily_burn_rate BIGINT NOT NULL,
    projected_expense BIGINT NOT NULL,
    upcoming_obligations BIGINT DEFAULT 0,
    projected_balance BIGINT NOT NULL,
    risk_level VARCHAR(20),
    insight TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cashflow_predictions_user_id ON cashflow_predictions(user_id);
CREATE INDEX IF NOT EXISTS idx_cashflow_predictions_target_date ON cashflow_predictions(target_date);
