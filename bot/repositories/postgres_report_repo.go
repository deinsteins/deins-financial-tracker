package repositories

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"finance-bot/bot/models"
)

type postgresReportRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresReportRepository(pool *pgxpool.Pool) ReportRepository {
	return &postgresReportRepository{pool: pool}
}

func (r *postgresReportRepository) Create(report *models.Report) error {
	query := `INSERT INTO reports (user_id, report_type, content) 
	          VALUES ($1, $2, $3) 
	          RETURNING id, created_at`
	err := r.pool.QueryRow(context.Background(), query, report.UserID, report.ReportType, report.Content).Scan(&report.ID, &report.CreatedAt)
	if err != nil {
		return fmt.Errorf("postgres_report_repo: failed to create report: %w", err)
	}
	return nil
}

func (r *postgresReportRepository) GetByUserID(userID string) ([]*models.Report, error) {
	query := `SELECT id, user_id, report_type, content, created_at 
	          FROM reports 
	          WHERE user_id = $1 
	          ORDER BY created_at DESC`
	rows, err := r.pool.Query(context.Background(), query, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres_report_repo: failed to query reports: %w", err)
	}
	defer rows.Close()

	var reports []*models.Report
	for rows.Next() {
		var report models.Report
		err := rows.Scan(
			&report.ID,
			&report.UserID,
			&report.ReportType,
			&report.Content,
			&report.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres_report_repo: failed to scan report: %w", err)
		}
		reports = append(reports, &report)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres_report_repo: row iteration error: %w", err)
	}

	return reports, nil
}
