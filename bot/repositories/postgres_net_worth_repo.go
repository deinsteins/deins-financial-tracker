package repositories

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"finance-bot/bot/models"
)

type postgresNetWorthRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresNetWorthRepository(pool *pgxpool.Pool) NetWorthRepository {
	return &postgresNetWorthRepository{pool: pool}
}

func (r *postgresNetWorthRepository) CreateAsset(asset *models.Asset) error {
	query := `INSERT INTO assets (user_id, asset_type, name, amount, currency, notes)
	          VALUES ($1, $2, $3, $4, $5, $6)
	          RETURNING id, created_at, updated_at`
	
	currency := asset.Currency
	if currency == "" {
		currency = "IDR"
	}

	err := r.pool.QueryRow(context.Background(), query,
		asset.UserID, asset.AssetType, asset.Name, asset.Amount, currency, asset.Notes,
	).Scan(&asset.ID, &asset.CreatedAt, &asset.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres_net_worth_repo: failed to create asset: %w", err)
	}
	return nil
}

func (r *postgresNetWorthRepository) UpdateAssetAmount(id string, amount int64) error {
	query := `UPDATE assets
	          SET amount = $1, updated_at = CURRENT_TIMESTAMP
	          WHERE id = $2`
	_, err := r.pool.Exec(context.Background(), query, amount, id)
	if err != nil {
		return fmt.Errorf("postgres_net_worth_repo: failed to update asset amount: %w", err)
	}
	return nil
}

func (r *postgresNetWorthRepository) DeleteAsset(id string) error {
	query := `DELETE FROM assets WHERE id = $1`
	_, err := r.pool.Exec(context.Background(), query, id)
	if err != nil {
		return fmt.Errorf("postgres_net_worth_repo: failed to delete asset: %w", err)
	}
	return nil
}

func (r *postgresNetWorthRepository) GetAssetsByUser(userID string) ([]*models.Asset, error) {
	query := `SELECT id, user_id, asset_type, name, amount, currency, notes, created_at, updated_at
	          FROM assets
	          WHERE user_id = $1
	          ORDER BY created_at DESC`
	rows, err := r.pool.Query(context.Background(), query, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres_net_worth_repo: failed to query assets: %w", err)
	}
	defer rows.Close()

	var assets []*models.Asset
	for rows.Next() {
		var a models.Asset
		err := rows.Scan(&a.ID, &a.UserID, &a.AssetType, &a.Name, &a.Amount, &a.Currency, &a.Notes, &a.CreatedAt, &a.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("postgres_net_worth_repo: failed to scan asset: %w", err)
		}
		assets = append(assets, &a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres_net_worth_repo: row iteration error: %w", err)
	}
	return assets, nil
}

func (r *postgresNetWorthRepository) CreateLiability(liability *models.Liability) error {
	query := `INSERT INTO liabilities (user_id, liability_type, name, amount, currency, due_date, notes)
	          VALUES ($1, $2, $3, $4, $5, $6, $7)
	          RETURNING id, created_at, updated_at`

	currency := liability.Currency
	if currency == "" {
		currency = "IDR"
	}

	err := r.pool.QueryRow(context.Background(), query,
		liability.UserID, liability.LiabilityType, liability.Name, liability.Amount, currency, liability.DueDate, liability.Notes,
	).Scan(&liability.ID, &liability.CreatedAt, &liability.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres_net_worth_repo: failed to create liability: %w", err)
	}
	return nil
}

func (r *postgresNetWorthRepository) UpdateLiabilityAmount(id string, amount int64) error {
	query := `UPDATE liabilities
	          SET amount = $1, updated_at = CURRENT_TIMESTAMP
	          WHERE id = $2`
	_, err := r.pool.Exec(context.Background(), query, amount, id)
	if err != nil {
		return fmt.Errorf("postgres_net_worth_repo: failed to update liability amount: %w", err)
	}
	return nil
}

func (r *postgresNetWorthRepository) DeleteLiability(id string) error {
	query := `DELETE FROM liabilities WHERE id = $1`
	_, err := r.pool.Exec(context.Background(), query, id)
	if err != nil {
		return fmt.Errorf("postgres_net_worth_repo: failed to delete liability: %w", err)
	}
	return nil
}

func (r *postgresNetWorthRepository) GetLiabilitiesByUser(userID string) ([]*models.Liability, error) {
	query := `SELECT id, user_id, liability_type, name, amount, currency, due_date, notes, created_at, updated_at
	          FROM liabilities
	          WHERE user_id = $1
	          ORDER BY created_at DESC`
	rows, err := r.pool.Query(context.Background(), query, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres_net_worth_repo: failed to query liabilities: %w", err)
	}
	defer rows.Close()

	var liabilities []*models.Liability
	for rows.Next() {
		var l models.Liability
		err := rows.Scan(&l.ID, &l.UserID, &l.LiabilityType, &l.Name, &l.Amount, &l.Currency, &l.DueDate, &l.Notes, &l.CreatedAt, &l.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("postgres_net_worth_repo: failed to scan liability: %w", err)
		}
		liabilities = append(liabilities, &l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres_net_worth_repo: row iteration error: %w", err)
	}
	return liabilities, nil
}

func (r *postgresNetWorthRepository) CalculateNetWorth(userID string) (int64, int64, error) {
	query := `
		SELECT 
			COALESCE((SELECT SUM(amount) FROM assets WHERE user_id = $1), 0) as total_assets,
			COALESCE((SELECT SUM(amount) FROM liabilities WHERE user_id = $1), 0) as total_liabilities`
	
	var totalAssets, totalLiabilities int64
	err := r.pool.QueryRow(context.Background(), query, userID).Scan(&totalAssets, &totalLiabilities)
	if err != nil {
		return 0, 0, fmt.Errorf("postgres_net_worth_repo: failed to calculate net worth: %w", err)
	}
	return totalAssets, totalLiabilities, nil
}

func (r *postgresNetWorthRepository) CreateNetWorthSnapshot(snapshot *models.NetWorthSnapshot) error {
	query := `INSERT INTO net_worth_snapshots (user_id, total_assets, total_liabilities, net_worth, snapshot_date)
	          VALUES ($1, $2, $3, $4, $5)
	          ON CONFLICT (user_id, snapshot_date) DO UPDATE
	          SET total_assets = EXCLUDED.total_assets,
	              total_liabilities = EXCLUDED.total_liabilities,
	              net_worth = EXCLUDED.net_worth
	          RETURNING id, created_at`
	err := r.pool.QueryRow(context.Background(), query,
		snapshot.UserID, snapshot.TotalAssets, snapshot.TotalLiabilities, snapshot.NetWorth, snapshot.SnapshotDate,
	).Scan(&snapshot.ID, &snapshot.CreatedAt)
	if err != nil {
		return fmt.Errorf("postgres_net_worth_repo: failed to upsert net worth snapshot: %w", err)
	}
	return nil
}

func (r *postgresNetWorthRepository) GetNetWorthHistory(userID string) ([]*models.NetWorthSnapshot, error) {
	query := `SELECT id, user_id, total_assets, total_liabilities, net_worth, snapshot_date, created_at
	          FROM net_worth_snapshots
	          WHERE user_id = $1
	          ORDER BY snapshot_date ASC`
	rows, err := r.pool.Query(context.Background(), query, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres_net_worth_repo: failed to query net worth snapshots: %w", err)
	}
	defer rows.Close()

	var history []*models.NetWorthSnapshot
	for rows.Next() {
		var s models.NetWorthSnapshot
		err := rows.Scan(&s.ID, &s.UserID, &s.TotalAssets, &s.TotalLiabilities, &s.NetWorth, &s.SnapshotDate, &s.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("postgres_net_worth_repo: failed to scan net worth snapshot: %w", err)
		}
		history = append(history, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres_net_worth_repo: row iteration error: %w", err)
	}
	return history, nil
}
