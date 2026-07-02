package models

import (
	"time"
)

type User struct {
	ID                  string    `json:"id"`
	TelegramID          int64     `json:"telegram_id"`
	FullName            string    `json:"full_name"`
	MonthlyBudget       int64     `json:"monthly_budget"`
	BudgetCycleStartDay int       `json:"budget_cycle_start_day"`
	CreatedAt           time.Time `json:"created_at"`
}

type Transaction struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	Type            string    `json:"type"` // "expense" or "income"
	Category        string    `json:"category"`
	Amount          int64     `json:"amount"` // in cents
	Description     string    `json:"description"`
	WalletID        *string   `json:"wallet_id,omitempty"`
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

type Wallet struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"created_at"`
}

type Debt struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	PersonName string     `json:"person_name"`
	Direction  string     `json:"direction"` // "payable" or "receivable"
	Amount     int64      `json:"amount"`
	PaidAmount int64      `json:"paid_amount"`
	Description string    `json:"description"`
	Status     string     `json:"status"` // "active", "partial", "paid", "cancelled"
	DueDate    *time.Time `json:"due_date,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type DebtPayment struct {
	ID     string    `json:"id"`
	DebtID string    `json:"debt_id"`
	Amount int64     `json:"amount"`
	Note   string    `json:"note"`
	PaidAt time.Time `json:"paid_at"`
}

type Asset struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	AssetType string    `json:"asset_type"`
	Name      string    `json:"name"`
	Amount    int64     `json:"amount"`
	Currency  string    `json:"currency"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Liability struct {
	ID            string     `json:"id"`
	UserID        string     `json:"user_id"`
	LiabilityType string     `json:"liability_type"`
	Name          string     `json:"name"`
	Amount        int64      `json:"amount"`
	Currency      string     `json:"currency"`
	DueDate       *time.Time `json:"due_date,omitempty"`
	Notes         string     `json:"notes"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type NetWorthSnapshot struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	TotalAssets      int64     `json:"total_assets"`
	TotalLiabilities int64     `json:"total_liabilities"`
	NetWorth         int64     `json:"net_worth"`
	SnapshotDate     time.Time `json:"snapshot_date"`
	CreatedAt        time.Time `json:"created_at"`
}

