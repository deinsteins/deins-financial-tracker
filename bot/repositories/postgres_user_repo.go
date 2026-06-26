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
	return &postgresUserRepository{pool: pool}
}

func (r *postgresUserRepository) Create(user *models.User) error {
	var query string
	var err error

	if user.ID == "" {
		query = `INSERT INTO users (telegram_id, full_name, monthly_budget) 
		         VALUES ($1, $2, $3) 
		         RETURNING id, created_at`
		err = r.pool.QueryRow(context.Background(), query, user.TelegramID, user.FullName, user.MonthlyBudget).Scan(&user.ID, &user.CreatedAt)
	} else {
		query = `INSERT INTO users (id, telegram_id, full_name, monthly_budget) 
		         VALUES ($1, $2, $3, $4) 
		         RETURNING created_at`
		err = r.pool.QueryRow(context.Background(), query, user.ID, user.TelegramID, user.FullName, user.MonthlyBudget).Scan(&user.CreatedAt)
	}

	if err != nil {
		return fmt.Errorf("postgres_user_repo: failed to create user: %w", err)
	}
	return nil
}

func (r *postgresUserRepository) GetByTelegramID(telegramID int64) (*models.User, error) {
	query := `SELECT id, telegram_id, full_name, monthly_budget, created_at 
	          FROM users 
	          WHERE telegram_id = $1`

	var user models.User
	err := r.pool.QueryRow(context.Background(), query, telegramID).Scan(
		&user.ID,
		&user.TelegramID,
		&user.FullName,
		&user.MonthlyBudget,
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
