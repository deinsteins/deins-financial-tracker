package repositories

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"finance-bot/bot/models"
)

type postgresTransactionRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresTransactionRepository(pool *pgxpool.Pool) TransactionRepository {
	return &postgresTransactionRepository{pool: pool}
}

func (r *postgresTransactionRepository) Create(tx *models.Transaction) error {
	var query string
	var err error

	if tx.ID == "" {
		query = `INSERT INTO transactions (user_id, type, category, amount, description, wallet_id) 
		         VALUES ($1, $2, $3, $4, $5, $6) 
		         RETURNING id, transaction_date, created_at`
		err = r.pool.QueryRow(context.Background(), query, tx.UserID, tx.Type, tx.Category, tx.Amount, tx.Description, tx.WalletID).Scan(&tx.ID, &tx.TransactionDate, &tx.CreatedAt)
	} else {
		if tx.TransactionDate.IsZero() {
			tx.TransactionDate = tx.TransactionDate.UTC()
		}
		query = `INSERT INTO transactions (id, user_id, type, category, amount, description, wallet_id, transaction_date) 
		         VALUES ($1, $2, $3, $4, $5, $6, $7, $8) 
		         RETURNING created_at`
		err = r.pool.QueryRow(context.Background(), query, tx.ID, tx.UserID, tx.Type, tx.Category, tx.Amount, tx.Description, tx.WalletID, tx.TransactionDate).Scan(&tx.CreatedAt)
	}

	if err != nil {
		return fmt.Errorf("postgres_transaction_repo: failed to create transaction: %w", err)
	}
	return nil
}

func (r *postgresTransactionRepository) GetByUserID(userID string) ([]*models.Transaction, error) {
	query := `SELECT id, user_id, type, category, amount, description, wallet_id, transaction_date, created_at 
	          FROM transactions 
	          WHERE user_id = $1 
	          ORDER BY transaction_date DESC`
	return r.queryTransactions(query, userID)
}

func (r *postgresTransactionRepository) GetToday(userID string, tz string) ([]*models.Transaction, error) {
	query := `SELECT id, user_id, type, category, amount, description, wallet_id, transaction_date, created_at 
	          FROM transactions 
	          WHERE user_id = $1 AND (transaction_date AT TIME ZONE $2)::date = (CURRENT_TIMESTAMP AT TIME ZONE $2)::date 
	          ORDER BY transaction_date DESC`
	return r.queryTransactions(query, userID, tz)
}

func (r *postgresTransactionRepository) GetMonth(userID string, startTime time.Time, endTime time.Time) ([]*models.Transaction, error) {
	query := `SELECT id, user_id, type, category, amount, description, wallet_id, transaction_date, created_at 
	          FROM transactions 
	          WHERE user_id = $1 AND transaction_date >= $2 AND transaction_date <= $3 
	          ORDER BY transaction_date DESC`
	return r.queryTransactions(query, userID, startTime, endTime)
}

func (r *postgresTransactionRepository) queryTransactions(query string, args ...interface{}) ([]*models.Transaction, error) {
	rows, err := r.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres_transaction_repo: failed to execute query: %w", err)
	}
	defer rows.Close()

	var transactions []*models.Transaction
	for rows.Next() {
		var tx models.Transaction
		err := rows.Scan(
			&tx.ID,
			&tx.UserID,
			&tx.Type,
			&tx.Category,
			&tx.Amount,
			&tx.Description,
			&tx.WalletID,
			&tx.TransactionDate,
			&tx.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres_transaction_repo: failed to scan transaction: %w", err)
		}
		transactions = append(transactions, &tx)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres_transaction_repo: row iteration error: %w", err)
	}

	return transactions, nil
}

func (r *postgresTransactionRepository) GetNetSavings(userID string) (int64, error) {
	query := `SELECT COALESCE(SUM(CASE WHEN type = 'income' THEN amount ELSE -amount END), 0) 
	          FROM transactions 
	          WHERE user_id = $1`
	var netSavings int64
	err := r.pool.QueryRow(context.Background(), query, userID).Scan(&netSavings)
	if err != nil {
		return 0, fmt.Errorf("postgres_transaction_repo: failed to calculate net savings: %w", err)
	}
	return netSavings, nil
}

func (r *postgresTransactionRepository) GetByID(id string) (*models.Transaction, error) {
	query := `SELECT id, user_id, type, category, amount, description, wallet_id, transaction_date, created_at 
	          FROM transactions 
	          WHERE id = $1`
	txs, err := r.queryTransactions(query, id)
	if err != nil {
		return nil, fmt.Errorf("postgres_transaction_repo: failed to get transaction by id: %w", err)
	}
	if len(txs) == 0 {
		return nil, nil
	}
	return txs[0], nil
}

func (r *postgresTransactionRepository) Delete(id string) error {
	query := `DELETE FROM transactions WHERE id = $1`
	_, err := r.pool.Exec(context.Background(), query, id)
	if err != nil {
		return fmt.Errorf("postgres_transaction_repo: failed to delete transaction: %w", err)
	}
	return nil
}
