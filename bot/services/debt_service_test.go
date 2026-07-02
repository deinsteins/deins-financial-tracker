package services

import (
	"strings"
	"testing"
	"time"

	"finance-bot/bot/models"
	"finance-bot/bot/repositories"
)

type fakeUserRepo struct {
	user *models.User
}

func (f *fakeUserRepo) Create(user *models.User) error                         { return nil }
func (f *fakeUserRepo) GetByTelegramID(telegramID int64) (*models.User, error) { return f.user, nil }
func (f *fakeUserRepo) UpdateBudget(userID string, budget int64) error         { return nil }
func (f *fakeUserRepo) UpdateCycleStartDay(userID string, startDay int) error  { return nil }

type fakeDebtRepo struct {
	activeDebts                   []*models.Debt
	totalPayable, totalReceivable int64
}

func (f *fakeDebtRepo) CreateDebt(debt *models.Debt) error { return nil }
func (f *fakeDebtRepo) GetDebtsByUser(userID string) ([]*models.Debt, error) {
	return f.activeDebts, nil
}
func (f *fakeDebtRepo) GetActiveDebtsByUser(userID string) ([]*models.Debt, error) {
	return f.activeDebts, nil
}
func (f *fakeDebtRepo) GetDebtByPersonName(userID string, personName string) ([]*models.Debt, error) {
	return nil, nil
}
func (f *fakeDebtRepo) AddDebtPayment(payment *models.DebtPayment, newPaidAmount int64, newStatus string) error {
	return nil
}
func (f *fakeDebtRepo) MarkDebtAsPaid(debtID string) error { return nil }
func (f *fakeDebtRepo) CancelDebt(debtID string) error     { return nil }
func (f *fakeDebtRepo) GetDebtSummary(userID string) (int64, int64, error) {
	return f.totalPayable, f.totalReceivable, nil
}
func (f *fakeDebtRepo) GetDueDebtsForReminders(dueOnOrBefore time.Time) ([]*repositories.DueDebt, error) {
	return nil, nil
}

func newTestFinanceService(t *testing.T, debts []*models.Debt, totalPayable, totalReceivable int64) FinanceService {
	t.Helper()
	t.Setenv("TZ", "UTC")

	userRepo := &fakeUserRepo{user: &models.User{ID: "user-1", TelegramID: 111}}
	debtRepo := &fakeDebtRepo{activeDebts: debts, totalPayable: totalPayable, totalReceivable: totalReceivable}

	return NewFinanceService(nil, nil, userRepo, nil, nil, nil, nil, nil, nil, debtRepo)
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestGetDebtSummary_Fields(t *testing.T) {
	today := time.Now().UTC()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	overdue := today.AddDate(0, 0, -3)
	notOverdue := today.AddDate(0, 0, 5)

	debts := []*models.Debt{
		{ID: "1", PersonName: "Andi", Direction: "receivable", Amount: 700000, DueDate: ptrTime(notOverdue)},
		{ID: "2", PersonName: "Citra", Direction: "receivable", Amount: 250000, DueDate: ptrTime(overdue)},
		{ID: "3", PersonName: "Dewi", Direction: "receivable", Amount: 150000},
		{ID: "4", PersonName: "Eka", Direction: "receivable", Amount: 100000},
		{ID: "5", PersonName: "Budi", Direction: "payable", Amount: 300000},
		{ID: "6", PersonName: "Fajar", Direction: "payable", Amount: 200000},
	}

	svc := newTestFinanceService(t, debts, 500000, 1200000)

	summary, err := svc.GetDebtSummary(111)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantLines := []string{
		"*Piutang:*",
		"Total: Rp1.200.000",
		"Aktif: 4 orang",
		"Terlambat: 1",
		"*Hutang:*",
		"Total: Rp500.000",
		"Aktif: 2 orang",
		"Terlambat: 0",
		"*Net Position:*",
		"+Rp700.000",
		"*Terbesar:*",
		"1. Andi - Piutang Rp700.000",
		"2. Budi - Hutang Rp300.000",
	}
	for _, want := range wantLines {
		if !strings.Contains(summary, want) {
			t.Errorf("expected summary to contain %q, got:\n%s", want, summary)
		}
	}
}

func TestGetDebtSummary_TopFiveCap(t *testing.T) {
	debts := []*models.Debt{
		{ID: "1", PersonName: "A", Direction: "receivable", Amount: 100000},
		{ID: "2", PersonName: "B", Direction: "receivable", Amount: 200000},
		{ID: "3", PersonName: "C", Direction: "receivable", Amount: 300000},
		{ID: "4", PersonName: "D", Direction: "payable", Amount: 400000},
		{ID: "5", PersonName: "E", Direction: "payable", Amount: 500000},
		{ID: "6", PersonName: "F", Direction: "payable", Amount: 600000}, // should be excluded (6th largest)
	}

	svc := newTestFinanceService(t, debts, 1500000, 600000)

	summary, err := svc.GetDebtSummary(111)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOrder := []string{
		"1. F - Hutang Rp600.000",
		"2. E - Hutang Rp500.000",
		"3. D - Hutang Rp400.000",
		"4. C - Piutang Rp300.000",
		"5. B - Piutang Rp200.000",
	}
	for _, want := range wantOrder {
		if !strings.Contains(summary, want) {
			t.Errorf("expected summary to contain %q, got:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "A - Piutang Rp100.000") {
		t.Errorf("expected the 6th-largest debt to be excluded from Terbesar, got:\n%s", summary)
	}
}

func TestGetDebtSummary_Empty(t *testing.T) {
	svc := newTestFinanceService(t, nil, 0, 0)

	summary, err := svc.GetDebtSummary(111)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(summary, "Belum ada hutang atau piutang aktif") {
		t.Errorf("expected empty-state message, got:\n%s", summary)
	}
}

func TestFormatRupiahCompact(t *testing.T) {
	cases := map[int64]string{
		0:       "Rp0",
		999:     "Rp999",
		1000:    "Rp1.000",
		200000:  "Rp200.000",
		1234567: "Rp1.234.567",
		-500000: "-Rp500.000",
	}
	for amount, want := range cases {
		if got := formatRupiahCompact(amount); got != want {
			t.Errorf("formatRupiahCompact(%d) = %q, want %q", amount, got, want)
		}
	}
}
