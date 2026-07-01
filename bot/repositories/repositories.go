package repositories

import (
	"time"

	"finance-bot/bot/models"
)

type UserRepository interface {
	Create(user *models.User) error
	GetByTelegramID(telegramID int64) (*models.User, error)
	UpdateBudget(userID string, budget int64) error
}

type TransactionRepository interface {
	Create(tx *models.Transaction) error
	GetByUserID(userID string) ([]*models.Transaction, error)
	GetToday(userID string, tz string) ([]*models.Transaction, error)
	GetMonth(userID string, tz string) ([]*models.Transaction, error)
	GetNetSavings(userID string) (int64, error)
}

type ReportRepository interface {
	Create(report *models.Report) error
	GetByUserID(userID string) ([]*models.Report, error)
}

type BudgetRepository interface {
	SetLimit(userID string, category string, amount int64) error
	GetLimits(userID string) (map[string]int64, error)
	GetLimit(userID string, category string) (int64, error)
}

type GoalRepository interface {
	Create(goal *models.Goal) error
	GetByUserID(userID string) ([]*models.Goal, error)
}

type WalletRepository interface {
	CreateDefaultWallets(userID string) error
	EnsureWallet(userID string, name string) (*models.Wallet, error)
	UpdateBalance(walletID string, amount int64) error
	GetByUserID(userID string) ([]*models.Wallet, error)
}

type ChatMemoryRepository interface {
	Append(userID string, role string, content string) error
	GetLastN(userID string, n int) ([]*models.ChatMessage, error)
}

type DebtRepository interface {
	CreateDebt(debt *models.Debt) error
	GetDebtsByUser(userID string) ([]*models.Debt, error)
	GetActiveDebtsByUser(userID string) ([]*models.Debt, error)
	GetDebtByPersonName(userID string, personName string) ([]*models.Debt, error)
	AddDebtPayment(payment *models.DebtPayment, newPaidAmount int64, newStatus string) error
	MarkDebtAsPaid(debtID string) error
	CancelDebt(debtID string) error
	GetDebtSummary(userID string) (totalPayable int64, totalReceivable int64, err error)
	GetDueDebtsForReminders(dueOnOrBefore time.Time) ([]*DueDebt, error)
}

// DueDebt pairs an active debt with the Telegram ID of the user who owns it,
// so the reminder scheduler can notify the right chat without a separate
// per-user lookup.
type DueDebt struct {
	Debt       *models.Debt
	TelegramID int64
}

// DebtReminderRepository tracks which due-date reminders have already been
// sent, so the daily scheduler never notifies a user twice for the same
// debt on the same day.
type DebtReminderRepository interface {
	// TryRecordReminder atomically records that a reminder of the given type
	// was sent for a debt on reminderDate. It returns false (without error)
	// if a reminder was already recorded for that debt on that date, so the
	// caller can skip sending a duplicate message.
	TryRecordReminder(debtID, reminderType string, reminderDate time.Time) (bool, error)
}

