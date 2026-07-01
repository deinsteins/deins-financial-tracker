package services

import (
	"fmt"
	"strings"
	"time"

	"finance-bot/bot/models"
)

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

	totalPayable, totalReceivable, err := s.debtRepo.GetDebtSummary(user.ID)
	if err != nil {
		return "", fmt.Errorf("failed to get debt summary: %w", err)
	}

	if len(debts) == 0 {
		return "📒 *Ringkasan Hutang/Piutang*\n\nBelum ada hutang atau piutang aktif nih bro! 🎉", nil
	}

	var sb strings.Builder
	sb.WriteString("📒 *Ringkasan Hutang/Piutang*\n\n")

	// Separate payable and receivable
	var payables, receivables []*models.Debt
	for _, d := range debts {
		if d.Direction == "payable" {
			payables = append(payables, d)
		} else {
			receivables = append(receivables, d)
		}
	}

	if len(payables) > 0 {
		sb.WriteString("💸 *Hutang (lu yang bayar):*\n")
		for _, d := range payables {
			remaining := d.Amount - d.PaidAmount
			line := fmt.Sprintf("  • *%s*: %s", d.PersonName, formatIDRCurrency(remaining))
			if d.PaidAmount > 0 {
				line += fmt.Sprintf(" (dibayar %s dari %s)", formatIDRCurrency(d.PaidAmount), formatIDRCurrency(d.Amount))
			}
			if d.DueDate != nil {
				line += fmt.Sprintf(" — jatuh tempo %s", d.DueDate.Format("02 Jan 2006"))
			}
			if d.Description != "" {
				line += fmt.Sprintf(" _%s_", d.Description)
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	if len(receivables) > 0 {
		sb.WriteString("💰 *Piutang (orang yang bayar ke lu):*\n")
		for _, d := range receivables {
			remaining := d.Amount - d.PaidAmount
			line := fmt.Sprintf("  • *%s*: %s", d.PersonName, formatIDRCurrency(remaining))
			if d.PaidAmount > 0 {
				line += fmt.Sprintf(" (diterima %s dari %s)", formatIDRCurrency(d.PaidAmount), formatIDRCurrency(d.Amount))
			}
			if d.DueDate != nil {
				line += fmt.Sprintf(" — jatuh tempo %s", d.DueDate.Format("02 Jan 2006"))
			}
			if d.Description != "" {
				line += fmt.Sprintf(" _%s_", d.Description)
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("📊 *Total Hutang Aktif:* %s\n", formatIDRCurrency(totalPayable)))
	sb.WriteString(fmt.Sprintf("📊 *Total Piutang Aktif:* %s\n", formatIDRCurrency(totalReceivable)))

	netPosition := totalReceivable - totalPayable
	if netPosition >= 0 {
		sb.WriteString(fmt.Sprintf("\n✅ *Posisi Bersih:* +%s (piutang lebih besar)", formatIDRCurrency(netPosition)))
	} else {
		sb.WriteString(fmt.Sprintf("\n⚠️ *Posisi Bersih:* %s (hutang lebih besar)", formatIDRCurrency(netPosition)))
	}

	return sb.String(), nil
}
