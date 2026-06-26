package repositories

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresCategoryBudgetRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresCategoryBudgetRepository(pool *pgxpool.Pool) CategoryBudgetRepository {
	// Initialize table schema synchronously
	ddl := `
	CREATE TABLE IF NOT EXISTS category_budgets (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		category VARCHAR(100) NOT NULL,
		amount BIGINT NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, category)
	);`
	_, err := pool.Exec(context.Background(), ddl)
	if err != nil {
		log.Printf("ERROR: failed to ensure category_budgets table: %v", err)
	}

	return &postgresCategoryBudgetRepository{pool: pool}
}

func (r *postgresCategoryBudgetRepository) SetLimit(userID string, category string, amount int64) error {
	query := `
	INSERT INTO category_budgets (user_id, category, amount)
	VALUES ($1, $2, $3)
	ON CONFLICT (user_id, category) DO UPDATE
	SET amount = EXCLUDED.amount`
	_, err := r.pool.Exec(context.Background(), query, userID, category, amount)
	if err != nil {
		return fmt.Errorf("postgres_category_budget_repo: failed to set category budget limit: %w", err)
	}
	return nil
}

func (r *postgresCategoryBudgetRepository) GetLimits(userID string) (map[string]int64, error) {
	query := `SELECT category, amount FROM category_budgets WHERE user_id = $1`
	rows, err := r.pool.Query(context.Background(), query, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres_category_budget_repo: failed to query category budgets: %w", err)
	}
	defer rows.Close()

	limits := make(map[string]int64)
	for rows.Next() {
		var category string
		var amount int64
		if err := rows.Scan(&category, &amount); err != nil {
			return nil, fmt.Errorf("postgres_category_budget_repo: failed to scan category budget: %w", err)
		}
		limits[category] = amount
	}
	return limits, nil
}

func (r *postgresCategoryBudgetRepository) GetLimit(userID string, category string) (int64, error) {
	query := `SELECT amount FROM category_budgets WHERE user_id = $1 AND category = $2`
	var amount int64
	err := r.pool.QueryRow(context.Background(), query, userID, category).Scan(&amount)
	if err != nil {
		// Return 0 (no limit set) if not found or on query error
		return 0, nil
	}
	return amount, nil
}
