package repositories

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"finance-bot/bot/models"
)

func TestPostgresChatMemoryRepository(t *testing.T) {
	// Connect to local database for integration test
	// Use environment variables or fallback to local docker defaults
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("DB_PORT")
	if dbPort == "" {
		dbPort = "5432"
	}
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "postgres"
	}
	dbPassword := os.Getenv("DB_PASSWORD")
	if dbPassword == "" {
		dbPassword = "postgres_secure_pass"
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "finance_db"
	}

	connStr := fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=disable",
		dbUser, dbPassword, dbHost, dbPort, dbName)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Skipping integration test: failed to connect to database: %v", err)
		return
	}
	defer pool.Close()

	// Verify ping
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("Skipping integration test: database ping failed: %v", err)
		return
	}

	// Create test repositories
	userRepo := NewPostgresUserRepository(pool)
	chatMemoryRepo := NewPostgresChatMemoryRepository(pool)

	// Create a test user
	testTelegramID := int64(999999999)
	// Clean up any old test user
	_, _ = pool.Exec(ctx, "DELETE FROM users WHERE telegram_id = $1", testTelegramID)

	testUser := &models.User{
		TelegramID:    testTelegramID,
		FullName:      "Test Bot User",
		MonthlyBudget: 0,
	}
	err = userRepo.Create(testUser)
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	defer func() {
		// Clean up user and chat messages cascade
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", testUser.ID)
	}()

	// 1. Verify initially history is empty
	history, err := chatMemoryRepo.GetLastN(testUser.ID, 20)
	if err != nil {
		t.Fatalf("failed to get empty history: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected 0 messages, got %d", len(history))
	}

	// 2. Append 25 messages and check pruning (limit to 20)
	for i := 1; i <= 25; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		content := fmt.Sprintf("Message number %d", i)
		err = chatMemoryRepo.Append(testUser.ID, role, content)
		if err != nil {
			t.Fatalf("failed to append message %d: %v", i, err)
		}
		// Introduce a very tiny sleep to guarantee unique timestamps in PostgreSQL (sometimes transaction times are identical)
		time.Sleep(2 * time.Millisecond)
	}

	// 3. Verify it pruned to exactly 20 messages
	history, err = chatMemoryRepo.GetLastN(testUser.ID, 20)
	if err != nil {
		t.Fatalf("failed to retrieve history: %v", err)
	}
	if len(history) != 20 {
		t.Errorf("expected exactly 20 messages, got %d", len(history))
	}

	// 4. Verify the messages are correct and sorted chronologically ASC (from 6 to 25)
	for idx, msg := range history {
		expectedNum := idx + 6 // Because messages 1-5 should have been pruned
		expectedContent := fmt.Sprintf("Message number %d", expectedNum)
		if msg.Content != expectedContent {
			t.Errorf("index %d: expected content %q, got %q", idx, expectedContent, msg.Content)
		}

		expectedRole := "user"
		if expectedNum%2 == 0 {
			expectedRole = "assistant"
		}
		if msg.Role != expectedRole {
			t.Errorf("index %d: expected role %q, got %q", idx, expectedRole, msg.Role)
		}
	}

	// 5. Verify GetLastN with small N works
	historySmall, err := chatMemoryRepo.GetLastN(testUser.ID, 5)
	if err != nil {
		t.Fatalf("failed to retrieve small history: %v", err)
	}
	if len(historySmall) != 5 {
		t.Errorf("expected exactly 5 messages, got %d", len(historySmall))
	}
	// The last 5 should be messages 21, 22, 23, 24, 25
	for idx, msg := range historySmall {
		expectedNum := idx + 21
		expectedContent := fmt.Sprintf("Message number %d", expectedNum)
		if msg.Content != expectedContent {
			t.Errorf("index %d: expected content %q, got %q", idx, expectedContent, msg.Content)
		}
	}
}
