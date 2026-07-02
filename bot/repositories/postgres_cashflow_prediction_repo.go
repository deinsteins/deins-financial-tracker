package repositories

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finance-bot/bot/models"
)

type postgresCashflowPredictionRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresCashflowPredictionRepository(pool *pgxpool.Pool) CashflowPredictionRepository {
	return &postgresCashflowPredictionRepository{pool: pool}
}

func (r *postgresCashflowPredictionRepository) CreatePrediction(prediction *models.CashflowPrediction) error {
	query := `INSERT INTO cashflow_predictions (
	              user_id, start_date, target_date, available_balance, 
	              daily_burn_rate, projected_expense, upcoming_obligations, 
	              projected_balance, risk_level, insight
	          )
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	          RETURNING id, created_at`

	err := r.pool.QueryRow(context.Background(), query,
		prediction.UserID, prediction.StartDate, prediction.TargetDate, prediction.AvailableBalance,
		prediction.DailyBurnRate, prediction.ProjectedExpense, prediction.UpcomingObligations,
		prediction.ProjectedBalance, prediction.RiskLevel, prediction.Insight,
	).Scan(&prediction.ID, &prediction.CreatedAt)

	if err != nil {
		return fmt.Errorf("postgres_cashflow_prediction_repo: failed to create cashflow prediction: %w", err)
	}
	return nil
}

func (r *postgresCashflowPredictionRepository) GetLatestPredictionByUser(userID string) (*models.CashflowPrediction, error) {
	query := `SELECT id, user_id, start_date, target_date, available_balance, 
	                 daily_burn_rate, projected_expense, upcoming_obligations, 
	                 projected_balance, risk_level, insight, created_at
	          FROM cashflow_predictions
	          WHERE user_id = $1
	          ORDER BY created_at DESC
	          LIMIT 1`

	var p models.CashflowPrediction
	err := r.pool.QueryRow(context.Background(), query, userID).Scan(
		&p.ID, &p.UserID, &p.StartDate, &p.TargetDate, &p.AvailableBalance,
		&p.DailyBurnRate, &p.ProjectedExpense, &p.UpcomingObligations,
		&p.ProjectedBalance, &p.RiskLevel, &p.Insight, &p.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres_cashflow_prediction_repo: failed to query latest prediction: %w", err)
	}
	return &p, nil
}

func (r *postgresCashflowPredictionRepository) GetPredictionHistoryByUser(userID string) ([]*models.CashflowPrediction, error) {
	query := `SELECT id, user_id, start_date, target_date, available_balance, 
	                 daily_burn_rate, projected_expense, upcoming_obligations, 
	                 projected_balance, risk_level, insight, created_at
	          FROM cashflow_predictions
	          WHERE user_id = $1
	          ORDER BY created_at DESC`

	rows, err := r.pool.Query(context.Background(), query, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres_cashflow_prediction_repo: failed to query prediction history: %w", err)
	}
	defer rows.Close()

	var history []*models.CashflowPrediction
	for rows.Next() {
		var p models.CashflowPrediction
		err := rows.Scan(
			&p.ID, &p.UserID, &p.StartDate, &p.TargetDate, &p.AvailableBalance,
			&p.DailyBurnRate, &p.ProjectedExpense, &p.UpcomingObligations,
			&p.ProjectedBalance, &p.RiskLevel, &p.Insight, &p.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres_cashflow_prediction_repo: failed to scan prediction row: %w", err)
		}
		history = append(history, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres_cashflow_prediction_repo: row iteration error: %w", err)
	}
	return history, nil
}
