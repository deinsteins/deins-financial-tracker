package repositories

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"finance-bot/bot/models"
)

type postgresDebtRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresDebtRepository(pool *pgxpool.Pool) DebtRepository {
	ddl := `
	CREATE TABLE IF NOT EXISTS debts (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		person_name VARCHAR(255) NOT NULL,
		direction VARCHAR(10) NOT NULL CHECK (direction IN ('payable', 'receivable')),
		amount BIGINT NOT NULL,
		paid_amount BIGINT NOT NULL DEFAULT 0,
		description TEXT,
		status VARCHAR(10) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'partial', 'paid', 'cancelled')),
		due_date DATE,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS debt_payments (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		debt_id UUID NOT NULL REFERENCES debts(id) ON DELETE CASCADE,
		amount BIGINT NOT NULL,
		note TEXT,
		paid_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);`
	_, err := pool.Exec(context.Background(), ddl)
	if err != nil {
		log.Printf("ERROR: failed to ensure debts/debt_payments tables: %v", err)
	}

	return &postgresDebtRepository{pool: pool}
}

func (r *postgresDebtRepository) CreateDebt(debt *models.Debt) error {
	query := `INSERT INTO debts (user_id, person_name, direction, amount, description, due_date)
	          VALUES ($1, $2, $3, $4, $5, $6)
	          RETURNING id, paid_amount, status, created_at, updated_at`
	err := r.pool.QueryRow(context.Background(), query,
		debt.UserID, debt.PersonName, debt.Direction, debt.Amount, debt.Description, debt.DueDate,
	).Scan(&debt.ID, &debt.PaidAmount, &debt.Status, &debt.CreatedAt, &debt.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres_debt_repo: failed to create debt: %w", err)
	}
	return nil
}

func (r *postgresDebtRepository) GetDebtsByUser(userID string) ([]*models.Debt, error) {
	query := `SELECT id, user_id, person_name, direction, amount, paid_amount, description, status, due_date, created_at, updated_at
	          FROM debts
	          WHERE user_id = $1
	          ORDER BY created_at DESC`
	return r.queryDebts(query, userID)
}

func (r *postgresDebtRepository) GetActiveDebtsByUser(userID string) ([]*models.Debt, error) {
	query := `SELECT id, user_id, person_name, direction, amount, paid_amount, description, status, due_date, created_at, updated_at
	          FROM debts
	          WHERE user_id = $1 AND status IN ('active', 'partial')
	          ORDER BY created_at DESC`
	return r.queryDebts(query, userID)
}

func (r *postgresDebtRepository) GetDebtByPersonName(userID string, personName string) ([]*models.Debt, error) {
	query := `SELECT id, user_id, person_name, direction, amount, paid_amount, description, status, due_date, created_at, updated_at
	          FROM debts
	          WHERE user_id = $1 AND LOWER(person_name) = LOWER($2) AND status IN ('active', 'partial')
	          ORDER BY created_at DESC`
	return r.queryDebts(query, userID, personName)
}

func (r *postgresDebtRepository) AddDebtPayment(payment *models.DebtPayment, newPaidAmount int64, newStatus string) error {
	tx, err := r.pool.Begin(context.Background())
	if err != nil {
		return fmt.Errorf("postgres_debt_repo: failed to begin transaction: %w", err)
	}
	defer tx.Rollback(context.Background())

	// Insert payment record
	paymentQuery := `INSERT INTO debt_payments (debt_id, amount, note)
	                  VALUES ($1, $2, $3)
	                  RETURNING id, paid_at`
	err = tx.QueryRow(context.Background(), paymentQuery,
		payment.DebtID, payment.Amount, payment.Note,
	).Scan(&payment.ID, &payment.PaidAt)
	if err != nil {
		return fmt.Errorf("postgres_debt_repo: failed to insert debt payment: %w", err)
	}

	// Update debt paid_amount and status atomically
	updateQuery := `UPDATE debts
	                SET paid_amount = $1, status = $2, updated_at = CURRENT_TIMESTAMP
	                WHERE id = $3`
	_, err = tx.Exec(context.Background(), updateQuery, newPaidAmount, newStatus, payment.DebtID)
	if err != nil {
		return fmt.Errorf("postgres_debt_repo: failed to update debt after payment: %w", err)
	}

	if err := tx.Commit(context.Background()); err != nil {
		return fmt.Errorf("postgres_debt_repo: failed to commit payment transaction: %w", err)
	}
	return nil
}

func (r *postgresDebtRepository) MarkDebtAsPaid(debtID string) error {
	query := `UPDATE debts
	          SET status = 'paid', paid_amount = amount, updated_at = CURRENT_TIMESTAMP
	          WHERE id = $1`
	_, err := r.pool.Exec(context.Background(), query, debtID)
	if err != nil {
		return fmt.Errorf("postgres_debt_repo: failed to mark debt as paid: %w", err)
	}
	return nil
}

func (r *postgresDebtRepository) CancelDebt(debtID string) error {
	query := `UPDATE debts
	          SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
	          WHERE id = $1`
	_, err := r.pool.Exec(context.Background(), query, debtID)
	if err != nil {
		return fmt.Errorf("postgres_debt_repo: failed to cancel debt: %w", err)
	}
	return nil
}

func (r *postgresDebtRepository) GetDebtSummary(userID string) (int64, int64, error) {
	query := `SELECT
	            COALESCE(SUM(CASE WHEN direction = 'payable' THEN amount - paid_amount ELSE 0 END), 0),
	            COALESCE(SUM(CASE WHEN direction = 'receivable' THEN amount - paid_amount ELSE 0 END), 0)
	          FROM debts
	          WHERE user_id = $1 AND status IN ('active', 'partial')`
	var totalPayable, totalReceivable int64
	err := r.pool.QueryRow(context.Background(), query, userID).Scan(&totalPayable, &totalReceivable)
	if err != nil {
		return 0, 0, fmt.Errorf("postgres_debt_repo: failed to get debt summary: %w", err)
	}
	return totalPayable, totalReceivable, nil
}

func (r *postgresDebtRepository) queryDebts(query string, args ...interface{}) ([]*models.Debt, error) {
	rows, err := r.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres_debt_repo: failed to execute query: %w", err)
	}
	defer rows.Close()

	var debts []*models.Debt
	for rows.Next() {
		var d models.Debt
		err := rows.Scan(
			&d.ID, &d.UserID, &d.PersonName, &d.Direction,
			&d.Amount, &d.PaidAmount, &d.Description, &d.Status,
			&d.DueDate, &d.CreatedAt, &d.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres_debt_repo: failed to scan debt: %w", err)
		}
		debts = append(debts, &d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres_debt_repo: row iteration error: %w", err)
	}

	return debts, nil
}
