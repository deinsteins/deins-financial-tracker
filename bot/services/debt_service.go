package services

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"finance-bot/bot/models"
)

func (s *financeService) ParseDebtText(text string) (*DebtParseResponse, error) {
	return s.debtAI.ParseDebt(text)
}

func (s *financeService) AddDebt(telegramID int64, personName, direction string, amount int64, description string, dueDate *time.Time) (*models.Debt, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return nil, err
	}

	if direction != "payable" && direction != "receivable" {
		return nil, fmt.Errorf("invalid direction: must be 'payable' or 'receivable'")
	}
	if amount <= 0 {
		return nil, fmt.Errorf("invalid amount: must be positive")
	}

	debt := &models.Debt{
		UserID:      user.ID,
		PersonName:  personName,
		Direction:   direction,
		Amount:      amount,
		Description: description,
		DueDate:     dueDate,
	}

	if err := s.debtRepo.CreateDebt(debt); err != nil {
		return nil, fmt.Errorf("failed to create debt: %w", err)
	}

	return debt, nil
}

func (s *financeService) GetDebts(telegramID int64, activeOnly bool) ([]*models.Debt, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return nil, err
	}

	if activeOnly {
		return s.debtRepo.GetActiveDebtsByUser(user.ID)
	}
	return s.debtRepo.GetDebtsByUser(user.ID)
}

func (s *financeService) GetDebtsByPersonName(telegramID int64, personName string) ([]*models.Debt, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return nil, err
	}

	return s.debtRepo.GetDebtByPersonName(user.ID, personName)
}

func (s *financeService) PayDebt(telegramID int64, debtID string, amount int64, note string) (*models.DebtPayment, *models.Debt, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return nil, nil, err
	}

	// Find the debt and verify it belongs to the user
	debts, err := s.debtRepo.GetDebtsByUser(user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch debts: %w", err)
	}

	var debt *models.Debt
	for _, d := range debts {
		if d.ID == debtID {
			debt = d
			break
		}
	}
	if debt == nil {
		return nil, nil, fmt.Errorf("debt not found")
	}
	if debt.Status == "paid" {
		return nil, nil, fmt.Errorf("debt is already fully paid")
	}
	if debt.Status == "cancelled" {
		return nil, nil, fmt.Errorf("debt has been cancelled")
	}
	if amount <= 0 {
		return nil, nil, fmt.Errorf("payment amount must be positive")
	}

	remaining := debt.Amount - debt.PaidAmount
	if amount > remaining {
		return nil, nil, fmt.Errorf("payment amount (%d) exceeds remaining debt (%d)", amount, remaining)
	}

	newPaidAmount := debt.PaidAmount + amount
	newRemaining := debt.Amount - newPaidAmount

	var newStatus string
	if newRemaining <= 0 {
		newStatus = "paid"
	} else {
		newStatus = "partial"
	}

	payment := &models.DebtPayment{
		DebtID: debtID,
		Amount: amount,
		Note:   note,
	}

	if err := s.debtRepo.AddDebtPayment(payment, newPaidAmount, newStatus); err != nil {
		return nil, nil, fmt.Errorf("failed to record payment: %w", err)
	}

	// Update the local debt copy to reflect the new state
	debt.PaidAmount = newPaidAmount
	debt.Status = newStatus

	return payment, debt, nil
}

func (s *financeService) SettleDebt(telegramID int64, debtID string) error {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return err
	}

	debts, err := s.debtRepo.GetDebtsByUser(user.ID)
	if err != nil {
		return fmt.Errorf("failed to fetch debts: %w", err)
	}

	found := false
	for _, d := range debts {
		if d.ID == debtID {
			found = true
			if d.Status == "paid" {
				return fmt.Errorf("debt is already paid")
			}
			if d.Status == "cancelled" {
				return fmt.Errorf("debt has been cancelled")
			}
			break
		}
	}
	if !found {
		return fmt.Errorf("debt not found")
	}

	return s.debtRepo.MarkDebtAsPaid(debtID)
}

func (s *financeService) CancelDebt(telegramID int64, debtID string) error {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return err
	}

	debts, err := s.debtRepo.GetDebtsByUser(user.ID)
	if err != nil {
		return fmt.Errorf("failed to fetch debts: %w", err)
	}

	found := false
	for _, d := range debts {
		if d.ID == debtID {
			found = true
			if d.Status == "paid" {
				return fmt.Errorf("cannot cancel a paid debt")
			}
			if d.Status == "cancelled" {
				return fmt.Errorf("debt is already cancelled")
			}
			break
		}
	}
	if !found {
		return fmt.Errorf("debt not found")
	}

	return s.debtRepo.CancelDebt(debtID)
}

func (s *financeService) GetDebtSummary(telegramID int64) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	debts, err := s.debtRepo.GetActiveDebtsByUser(user.ID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch debts: %w", err)
	}

	if len(debts) == 0 {
		return "📊 *Ringkasan Hutang & Piutang*\n\nBelum ada hutang atau piutang aktif nih bro! 🎉", nil
	}

	totalPayable, totalReceivable, err := s.debtRepo.GetDebtSummary(user.ID)
	if err != nil {
		return "", fmt.Errorf("failed to get debt summary: %w", err)
	}

	// Separate payable and receivable, and count how many are overdue
	// (due_date strictly before today, in the app's configured timezone).
	today := time.Now().In(s.loc)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, s.loc)

	var payables, receivables []*models.Debt
	var overduePayables, overdueReceivables int
	for _, d := range debts {
		isOverdue := false
		if d.DueDate != nil {
			y, m, day := d.DueDate.Date()
			dueDateOnly := time.Date(y, m, day, 0, 0, 0, 0, s.loc)
			isOverdue = dueDateOnly.Before(today)
		}

		if d.Direction == "payable" {
			payables = append(payables, d)
			if isOverdue {
				overduePayables++
			}
		} else {
			receivables = append(receivables, d)
			if isOverdue {
				overdueReceivables++
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("📊 *Ringkasan Hutang & Piutang*\n\n")

	sb.WriteString("*Piutang:*\n")
	sb.WriteString(fmt.Sprintf("Total: %s\n", formatRupiahCompact(totalReceivable)))
	sb.WriteString(fmt.Sprintf("Aktif: %d orang\n", len(receivables)))
	sb.WriteString(fmt.Sprintf("Terlambat: %d\n\n", overdueReceivables))

	sb.WriteString("*Hutang:*\n")
	sb.WriteString(fmt.Sprintf("Total: %s\n", formatRupiahCompact(totalPayable)))
	sb.WriteString(fmt.Sprintf("Aktif: %d orang\n", len(payables)))
	sb.WriteString(fmt.Sprintf("Terlambat: %d\n\n", overduePayables))

	netPosition := totalReceivable - totalPayable
	sb.WriteString("*Net Position:*\n")
	if netPosition >= 0 {
		sb.WriteString(fmt.Sprintf("+%s", formatRupiahCompact(netPosition)))
	} else {
		sb.WriteString(formatRupiahCompact(netPosition))
	}

	// Top 5 largest active debts by remaining amount, across both directions.
	top := make([]*models.Debt, len(debts))
	copy(top, debts)
	sort.Slice(top, func(i, j int) bool {
		return (top[i].Amount - top[i].PaidAmount) > (top[j].Amount - top[j].PaidAmount)
	})
	if len(top) > 5 {
		top = top[:5]
	}

	sb.WriteString("\n\n*Terbesar:*\n\n")
	for i, d := range top {
		dirLabel := "Piutang"
		if d.Direction == "payable" {
			dirLabel = "Hutang"
		}
		remaining := d.Amount - d.PaidAmount
		sb.WriteString(fmt.Sprintf("%d. %s - %s %s", i+1, d.PersonName, dirLabel, formatRupiahCompact(remaining)))
		if i < len(top)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String(), nil
}

// formatRupiahCompact formats an amount as Rupiah without a space after "Rp"
// (e.g. "Rp200.000", "-Rp50.000"), matching the compact dashboard style.
func formatRupiahCompact(amount int64) string {
	isNegative := amount < 0
	if isNegative {
		amount = -amount
	}

	s := fmt.Sprintf("%d", amount)
	var res string
	if len(s) <= 3 {
		res = s
	} else {
		var bytes []byte
		n := 0
		for i := len(s) - 1; i >= 0; i-- {
			if n > 0 && n%3 == 0 {
				bytes = append([]byte{'.'}, bytes...)
			}
			bytes = append([]byte{s[i]}, bytes...)
			n++
		}
		res = string(bytes)
	}

	if isNegative {
		return "-Rp" + res
	}
	return "Rp" + res
}
