package repositories

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresDebtReminderRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresDebtReminderRepository(pool *pgxpool.Pool) DebtReminderRepository {
	ddl := `
	CREATE TABLE IF NOT EXISTS debt_reminders (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		debt_id UUID NOT NULL REFERENCES debts(id) ON DELETE CASCADE,
		reminder_type VARCHAR(20) NOT NULL CHECK (reminder_type IN ('due_today', 'due_tomorrow', 'overdue')),
		reminder_date DATE NOT NULL,
		sent_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (debt_id, reminder_date)
	);
	CREATE INDEX IF NOT EXISTS idx_debt_reminders_debt_id ON debt_reminders(debt_id);`
	_, err := pool.Exec(context.Background(), ddl)
	if err != nil {
		log.Printf("ERROR: failed to ensure debt_reminders table: %v", err)
	}

	return &postgresDebtReminderRepository{pool: pool}
}

func (r *postgresDebtReminderRepository) TryRecordReminder(debtID, reminderType string, reminderDate time.Time) (bool, error) {
	query := `INSERT INTO debt_reminders (debt_id, reminder_type, reminder_date)
	          VALUES ($1, $2, $3)
	          ON CONFLICT (debt_id, reminder_date) DO NOTHING`
	tag, err := r.pool.Exec(context.Background(), query, debtID, reminderType, reminderDate)
	if err != nil {
		return false, fmt.Errorf("postgres_debt_reminder_repo: failed to record reminder: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
