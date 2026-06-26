package repositories

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"finance-bot/bot/models"
)

type postgresChatMemoryRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresChatMemoryRepository(pool *pgxpool.Pool) ChatMemoryRepository {
	// Initialize table schema synchronously
	ddl := `
	CREATE TABLE IF NOT EXISTS chat_messages (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role VARCHAR(50) NOT NULL,
		content TEXT NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);`
	_, err := pool.Exec(context.Background(), ddl)
	if err != nil {
		log.Printf("ERROR: failed to ensure chat_messages table: %v", err)
	}

	return &postgresChatMemoryRepository{pool: pool}
}

func (r *postgresChatMemoryRepository) Append(userID string, role string, content string) error {
	ctx := context.Background()

	// 1. Insert the new chat message
	insertQuery := `
	INSERT INTO chat_messages (user_id, role, content)
	VALUES ($1, $2, $3)`
	_, err := r.pool.Exec(ctx, insertQuery, userID, role, content)
	if err != nil {
		return fmt.Errorf("postgres_chat_memory_repo: failed to insert chat message: %w", err)
	}

	// 2. Keep only the last 20 messages for this user (prune older ones)
	cleanupQuery := `
	DELETE FROM chat_messages
	WHERE user_id = $1 AND id NOT IN (
		SELECT id FROM chat_messages
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 20
	)`
	_, err = r.pool.Exec(ctx, cleanupQuery, userID)
	if err != nil {
		// Log cleanup error but don't abort since the main insert succeeded
		log.Printf("WARNING: postgres_chat_memory_repo: failed to cleanup old chat messages: %v", err)
	}

	return nil
}

func (r *postgresChatMemoryRepository) GetLastN(userID string, n int) ([]*models.ChatMessage, error) {
	// Query last N messages sorted DESC (newest first) in subquery, then sort them ASC (chronological) for the LLM context
	query := `
	SELECT id, user_id, role, content, created_at
	FROM (
		SELECT id, user_id, role, content, created_at
		FROM chat_messages
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	) sub
	ORDER BY created_at ASC`

	rows, err := r.pool.Query(context.Background(), query, userID, n)
	if err != nil {
		return nil, fmt.Errorf("postgres_chat_memory_repo: failed to query chat messages: %w", err)
	}
	defer rows.Close()

	var messages []*models.ChatMessage
	for rows.Next() {
		var msg models.ChatMessage
		err := rows.Scan(
			&msg.ID,
			&msg.UserID,
			&msg.Role,
			&msg.Content,
			&msg.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres_chat_memory_repo: failed to scan chat message: %w", err)
		}
		messages = append(messages, &msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres_chat_memory_repo: row iteration error: %w", err)
	}

	return messages, nil
}
