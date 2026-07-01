CREATE TABLE IF NOT EXISTS debt_reminders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    debt_id UUID NOT NULL REFERENCES debts(id) ON DELETE CASCADE,
    reminder_type VARCHAR(20) NOT NULL CHECK (reminder_type IN ('due_today', 'due_tomorrow', 'overdue')),
    reminder_date DATE NOT NULL,
    sent_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (debt_id, reminder_date)
);
CREATE INDEX IF NOT EXISTS idx_debt_reminders_debt_id ON debt_reminders(debt_id);
