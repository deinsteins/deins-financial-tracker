package repositories

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"finance-bot/bot/models"
)

type postgresGoalRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresGoalRepository(pool *pgxpool.Pool) GoalRepository {
	// Initialize table schema synchronously
	ddl := `
	CREATE TABLE IF NOT EXISTS financial_goals (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		target_amount BIGINT NOT NULL,
		deadline TIMESTAMP WITH TIME ZONE NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);`
	_, err := pool.Exec(context.Background(), ddl)
	if err != nil {
		log.Printf("ERROR: failed to ensure financial_goals table: %v", err)
	}

	return &postgresGoalRepository{pool: pool}
}

func (r *postgresGoalRepository) Create(goal *models.Goal) error {
	query := `
	INSERT INTO financial_goals (user_id, name, target_amount, deadline)
	VALUES ($1, $2, $3, $4)
	RETURNING id, created_at`
	err := r.pool.QueryRow(context.Background(), query, goal.UserID, goal.Name, goal.TargetAmount, goal.Deadline).Scan(&goal.ID, &goal.CreatedAt)
	if err != nil {
		return fmt.Errorf("postgres_goal_repo: failed to create goal: %w", err)
	}
	return nil
}

func (r *postgresGoalRepository) GetByUserID(userID string) ([]*models.Goal, error) {
	query := `
	SELECT id, user_id, name, target_amount, deadline, created_at
	FROM financial_goals
	WHERE user_id = $1
	ORDER BY created_at ASC`
	rows, err := r.pool.Query(context.Background(), query, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres_goal_repo: failed to query goals: %w", err)
	}
	defer rows.Close()

	var goals []*models.Goal
	for rows.Next() {
		var goal models.Goal
		err := rows.Scan(
			&goal.ID,
			&goal.UserID,
			&goal.Name,
			&goal.TargetAmount,
			&goal.Deadline,
			&goal.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres_goal_repo: failed to scan goal: %w", err)
		}
		goals = append(goals, &goal)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres_goal_repo: row iteration error: %w", err)
	}

	return goals, nil
}
