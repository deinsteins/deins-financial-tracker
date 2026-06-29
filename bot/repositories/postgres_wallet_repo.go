package repositories

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"finance-bot/bot/models"
)

type postgresWalletRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresWalletRepository(pool *pgxpool.Pool) WalletRepository {
	// Initialize wallets table schema
	ddl := `
	CREATE TABLE IF NOT EXISTS wallets (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name VARCHAR(100) NOT NULL,
		balance BIGINT NOT NULL DEFAULT 0,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, name)
	);`
	_, err := pool.Exec(context.Background(), ddl)
	if err != nil {
		log.Printf("ERROR: failed to ensure wallets table: %v", err)
	}

	// Alter transactions table to add wallet_id if not exists
	alterDDL := `
	ALTER TABLE transactions 
	ADD COLUMN IF NOT EXISTS wallet_id UUID REFERENCES wallets(id) ON DELETE SET NULL;`
	_, err = pool.Exec(context.Background(), alterDDL)
	if err != nil {
		log.Printf("ERROR: failed to alter transactions table for wallet_id: %v", err)
	}

	return &postgresWalletRepository{pool: pool}
}

func (r *postgresWalletRepository) CreateDefaultWallets(userID string) error {
	ctx := context.Background()
	defaults := []string{"cash", "bank", "ewallet"}
	
	for _, name := range defaults {
		query := `
		INSERT INTO wallets (user_id, name, balance)
		VALUES ($1, $2, 0)
		ON CONFLICT (user_id, name) DO NOTHING`
		_, err := r.pool.Exec(ctx, query, userID, name)
		if err != nil {
			return fmt.Errorf("postgres_wallet_repo: failed to seed default wallet %s: %w", name, err)
		}
	}
	return nil
}

func (r *postgresWalletRepository) EnsureWallet(userID string, name string) (*models.Wallet, error) {
	ctx := context.Background()
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "cash"
	}

	// Try select first
	querySel := `SELECT id, user_id, name, balance FROM wallets WHERE user_id = $1 AND name = $2`
	var wallet models.Wallet
	err := r.pool.QueryRow(ctx, querySel, userID, name).Scan(&wallet.ID, &wallet.UserID, &wallet.Name, &wallet.Balance)
	if err == nil {
		return &wallet, nil
	}

	// If not exists, insert it
	queryIns := `
	INSERT INTO wallets (user_id, name, balance)
	VALUES ($1, $2, 0)
	ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
	RETURNING id, user_id, name, balance`
	err = r.pool.QueryRow(ctx, queryIns, userID, name).Scan(&wallet.ID, &wallet.UserID, &wallet.Name, &wallet.Balance)
	if err != nil {
		return nil, fmt.Errorf("postgres_wallet_repo: failed to ensure wallet %s: %w", name, err)
	}
	return &wallet, nil
}

func (r *postgresWalletRepository) UpdateBalance(walletID string, amount int64) error {
	query := `UPDATE wallets SET balance = balance + $1 WHERE id = $2`
	_, err := r.pool.Exec(context.Background(), query, amount, walletID)
	if err != nil {
		return fmt.Errorf("postgres_wallet_repo: failed to update wallet balance: %w", err)
	}
	return nil
}

func (r *postgresWalletRepository) GetByUserID(userID string) ([]*models.Wallet, error) {
	query := `SELECT id, user_id, name, balance, created_at FROM wallets WHERE user_id = $1 ORDER BY name ASC`
	rows, err := r.pool.Query(context.Background(), query, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres_wallet_repo: failed to fetch wallets: %w", err)
	}
	defer rows.Close()

	var wallets []*models.Wallet
	for rows.Next() {
		var w models.Wallet
		err := rows.Scan(&w.ID, &w.UserID, &w.Name, &w.Balance, &w.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("postgres_wallet_repo: failed to scan wallet: %w", err)
		}
		wallets = append(wallets, &w)
	}
	return wallets, nil
}
