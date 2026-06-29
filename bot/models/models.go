package models

import (
	"time"
)

type User struct {
	ID            string    `json:"id"`
	TelegramID    int64     `json:"telegram_id"`
	FullName      string    `json:"full_name"`
	MonthlyBudget int64     `json:"monthly_budget"`
	CreatedAt     time.Time `json:"created_at"`
}

type Transaction struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	Type            string    `json:"type"` // "expense" or "income"
	Category        string    `json:"category"`
	Amount          int64     `json:"amount"` // in cents
	Description     string    `json:"description"`
	TransactionDate time.Time `json:"transaction_date"`
	CreatedAt       time.Time `json:"created_at"`
}

type Report struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	ReportType string    `json:"report_type"`
	Content    string    `json:"content"` // JSONB payload
	CreatedAt  time.Time `json:"created_at"`
}

type ChatMessage struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type Goal struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	Name         string    `json:"name"`
	TargetAmount int64     `json:"target_amount"`
	Deadline     time.Time `json:"deadline"`
	CreatedAt    time.Time `json:"created_at"`
}

