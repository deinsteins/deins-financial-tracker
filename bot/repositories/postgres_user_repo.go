package repositories

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finance-bot/bot/models"
)

type postgresUserRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresUserRepository(pool *pgxpool.Pool) UserRepository {
	// Alter users table to add budget_cycle_start_day if not exists
	alterDDL := `
	ALTER TABLE users 
	ADD COLUMN IF NOT EXISTS budget_cycle_start_day INT DEFAULT 1 CHECK (budget_cycle_start_day >= 1 AND budget_cycle_start_day <= 31);`
	_, err := pool.Exec(context.Background(), alterDDL)
	if err != nil {
		fmt.Printf("ERROR: failed to alter users table for budget_cycle_start_day: %v\n", err)
	}

	return &postgresUserRepository{pool: pool}
}

func (r *postgresUserRepository) Create(user *models.User) error {
	var query string
	var err error

	if user.BudgetCycleStartDay == 0 {
		user.BudgetCycleStartDay = 1
	}

	if user.ID == "" {
		query = `INSERT INTO users (telegram_id, full_name, monthly_budget, budget_cycle_start_day, payday_day) 
		         VALUES ($1, $2, $3, $4, $5) 
		         RETURNING id, created_at`
		err = r.pool.QueryRow(context.Background(), query, user.TelegramID, user.FullName, user.MonthlyBudget, user.BudgetCycleStartDay, user.PaydayDay).Scan(&user.ID, &user.CreatedAt)
	} else {
		query = `INSERT INTO users (id, telegram_id, full_name, monthly_budget, budget_cycle_start_day, payday_day) 
		         VALUES ($1, $2, $3, $4, $5, $6) 
		         RETURNING created_at`
		err = r.pool.QueryRow(context.Background(), query, user.ID, user.TelegramID, user.FullName, user.MonthlyBudget, user.BudgetCycleStartDay, user.PaydayDay).Scan(&user.CreatedAt)
	}

	if err != nil {
		return fmt.Errorf("postgres_user_repo: failed to create user: %w", err)
	}
	return nil
}

func (r *postgresUserRepository) GetByTelegramID(telegramID int64) (*models.User, error) {
	query := `SELECT id, telegram_id, full_name, monthly_budget, budget_cycle_start_day, payday_day, created_at 
	          FROM users 
	          WHERE telegram_id = $1`

	var user models.User
	err := r.pool.QueryRow(context.Background(), query, telegramID).Scan(
		&user.ID,
		&user.TelegramID,
		&user.FullName,
		&user.MonthlyBudget,
		&user.BudgetCycleStartDay,
		&user.PaydayDay,
		&user.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // Return nil, nil if user does not exist
		}
		return nil, fmt.Errorf("postgres_user_repo: failed to query user: %w", err)
	}

	return &user, nil
}

func (r *postgresUserRepository) UpdateBudget(userID string, budget int64) error {
	query := `UPDATE users SET monthly_budget = $1 WHERE id = $2`
	_, err := r.pool.Exec(context.Background(), query, budget, userID)
	if err != nil {
		return fmt.Errorf("postgres_user_repo: failed to update monthly budget: %w", err)
	}
	return nil
}

func (r *postgresUserRepository) UpdateCycleStartDay(userID string, startDay int) error {
	query := `UPDATE users SET budget_cycle_start_day = $1 WHERE id = $2`
	_, err := r.pool.Exec(context.Background(), query, startDay, userID)
	if err != nil {
		return fmt.Errorf("postgres_user_repo: failed to update budget cycle start day: %w", err)
	}
	return nil
}

func (r *postgresUserRepository) GetAllUsers() ([]*models.User, error) {
	query := `SELECT id, telegram_id, full_name, monthly_budget, budget_cycle_start_day, payday_day, created_at 
	          FROM users`
	rows, err := r.pool.Query(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("postgres_user_repo: failed to query all users: %w", err)
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID,
			&user.TelegramID,
			&user.FullName,
			&user.MonthlyBudget,
			&user.BudgetCycleStartDay,
			&user.PaydayDay,
			&user.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres_user_repo: failed to scan user row: %w", err)
		}
		users = append(users, &user)
	}
	return users, nil
}

func (r *postgresUserRepository) UpdatePaydayDay(userID string, paydayDay *int) error {
	query := `UPDATE users SET payday_day = $1 WHERE id = $2`
	_, err := r.pool.Exec(context.Background(), query, paydayDay, userID)
	if err != nil {
		return fmt.Errorf("postgres_user_repo: failed to update payday day: %w", err)
	}
	return nil
}
