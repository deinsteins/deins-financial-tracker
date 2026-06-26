package repositories

import (
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
	GetToday(userID string) ([]*models.Transaction, error)
	GetMonth(userID string) ([]*models.Transaction, error)
}

type ReportRepository interface {
	Create(report *models.Report) error
	GetByUserID(userID string) ([]*models.Report, error)
}
