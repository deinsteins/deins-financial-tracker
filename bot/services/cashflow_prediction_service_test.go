package services

import (
	"strings"
	"testing"
	"time"

	"finance-bot/bot/models"
)

type fakeTxRepo struct {
	txs []*models.Transaction
}

func (f *fakeTxRepo) Create(tx *models.Transaction) error { return nil }
func (f *fakeTxRepo) GetByID(id string) (*models.Transaction, error) { return nil, nil }
func (f *fakeTxRepo) GetByUserID(userID string) ([]*models.Transaction, error) { return f.txs, nil }
func (f *fakeTxRepo) GetToday(userID string, tz string) ([]*models.Transaction, error) { return nil, nil }
func (f *fakeTxRepo) GetMonth(userID string, startTime time.Time, endTime time.Time) ([]*models.Transaction, error) {
	var res []*models.Transaction
	for _, tx := range f.txs {
		if !tx.TransactionDate.Before(startTime) && tx.TransactionDate.Before(endTime) {
			res = append(res, tx)
		}
	}
	return res, nil
}
func (f *fakeTxRepo) GetNetSavings(userID string) (int64, error) { return 0, nil }
func (f *fakeTxRepo) Delete(id string) error { return nil }

func TestPredictCashflow_Calculations(t *testing.T) {
	loc := time.UTC
	now := time.Now().UTC().Truncate(24 * time.Hour)

	userRepo := &fakeUserRepo{user: &models.User{ID: "user-123", TelegramID: 12345}}
	netWorthRepo := &fakeNetWorthRepo{
		assets: []*models.Asset{
			{AssetType: "cash", Amount: 5000000},
			{AssetType: "bank", Amount: 10000000},
			{AssetType: "investment", Amount: 30000000}, // Should not be included in cash-like balance
		},
		liabilities: []*models.Liability{
			{Amount: 1000000, DueDate: ptrTime(now.AddDate(0, 0, 5))}, // included in upcoming obligations
			{Amount: 2000000, DueDate: ptrTime(now.AddDate(0, 0, 15))}, // excluded (after target date)
		},
	}

	debtRepo := &fakeDebtRepo{
		activeDebts: []*models.Debt{
			{Direction: "payable", Amount: 2000000, PaidAmount: 500000, DueDate: ptrTime(now.AddDate(0, 0, 3))}, // included (1.5M remaining)
			{Direction: "receivable", Amount: 1000000, PaidAmount: 0, DueDate: ptrTime(now.AddDate(0, 0, 3))}, // excluded from obligations
		},
	}

	txRepo := &fakeTxRepo{
		txs: []*models.Transaction{
			// Expenses in the last 14 days
			{Type: "expense", Amount: 700000, TransactionDate: now.AddDate(0, 0, -2)},
			{Type: "expense", Amount: 700000, TransactionDate: now.AddDate(0, 0, -5)},
			// Income (should not affect daily burn rate)
			{Type: "income", Amount: 10000000, TransactionDate: now.AddDate(0, 0, -3)},
		},
	}

	cashflowRepo := &fakeCashflowRepo{}

	svc := &financeService{
		userRepo:     userRepo,
		netWorthRepo: netWorthRepo,
		debtRepo:     debtRepo,
		txRepo:       txRepo,
		cashflowRepo: cashflowRepo,
		loc:          loc,
	}

	targetDate := now.AddDate(0, 0, 10) // 10 days projection
	pred, msg, err := svc.PredictCashflow(12345, targetDate)
	if err != nil {
		t.Fatalf("PredictCashflow failed: %v", err)
	}

	// 1. Available Balance = cash (5M) + bank (10M) = 15M
	if pred.AvailableBalance != 15000000 {
		t.Errorf("Expected AvailableBalance 15M, got %d", pred.AvailableBalance)
	}

	// 2. Daily Burn Rate = (700K + 700K) / 14 = 100K
	if pred.DailyBurnRate != 100000 {
		t.Errorf("Expected DailyBurnRate 100K, got %d", pred.DailyBurnRate)
	}

	// 3. Projected Expense = 100K * 10 = 1M
	if pred.ProjectedExpense != 1000000 {
		t.Errorf("Expected ProjectedExpense 1M, got %d", pred.ProjectedExpense)
	}

	// 4. Upcoming Obligations = payable debt remaining (1.5M) + liability (1M) = 2.5M
	if pred.UpcomingObligations != 2500000 {
		t.Errorf("Expected UpcomingObligations 2.5M, got %d", pred.UpcomingObligations)
	}

	// 5. Projected Balance = 15M - 1M - 2.5M = 11.5M
	if pred.ProjectedBalance != 11500000 {
		t.Errorf("Expected ProjectedBalance 11.5M, got %d", pred.ProjectedBalance)
	}

	// 6. Risk Level = 11.5M / 15M = ~76% (> 20%), so "safe"
	if pred.RiskLevel != "safe" {
		t.Errorf("Expected RiskLevel 'safe', got %q", pred.RiskLevel)
	}

	if !strings.Contains(msg, "Rp 15.000.000") {
		t.Errorf("Message output did not format Rupiah correctly: %s", msg)
	}
}

func TestPredictCashflow_NoHistory(t *testing.T) {
	loc := time.UTC
	now := time.Now().UTC().Truncate(24 * time.Hour)

	userRepo := &fakeUserRepo{user: &models.User{ID: "user-123", TelegramID: 12345}}
	netWorthRepo := &fakeNetWorthRepo{
		assets: []*models.Asset{
			{AssetType: "cash", Amount: 100000},
		},
	}
	debtRepo := &fakeDebtRepo{}
	txRepo := &fakeTxRepo{} // empty history
	cashflowRepo := &fakeCashflowRepo{}

	svc := &financeService{
		userRepo:     userRepo,
		netWorthRepo: netWorthRepo,
		debtRepo:     debtRepo,
		txRepo:       txRepo,
		cashflowRepo: cashflowRepo,
		loc:          loc,
	}

	targetDate := now.AddDate(0, 0, 5)
	pred, msg, err := svc.PredictCashflow(12345, targetDate)
	if err != nil {
		t.Fatalf("PredictCashflow failed: %v", err)
	}

	if pred.DailyBurnRate != 0 {
		t.Errorf("Expected daily burn rate 0 for empty history, got %d", pred.DailyBurnRate)
	}

	if !strings.Contains(msg, "Insight belum maksimal karena kamu belum memiliki riwayat transaksi") {
		t.Errorf("Expected message to handle no history gracefully: %s", msg)
	}
}
