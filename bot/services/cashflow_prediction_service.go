package services

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"finance-bot/bot/models"
)

func (s *financeService) PredictCashflow(telegramID int64, targetDate time.Time) (*models.CashflowPrediction, string, error) {
	// 1. Get user
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return nil, "", err
	}

	if targetDate.IsZero() {
		now := time.Now().In(s.loc)
		if user.PaydayDay != nil {
			payday := *user.PaydayDay
			if now.Day() < payday {
				targetDate = time.Date(now.Year(), now.Month(), payday, 23, 59, 59, 0, s.loc)
			} else {
				targetDate = time.Date(now.Year(), now.Month(), payday, 23, 59, 59, 0, s.loc).AddDate(0, 1, 0)
			}
		} else {
			targetDate = time.Date(now.Year(), now.Month(), 1, 23, 59, 59, 0, s.loc).AddDate(0, 1, -1)
		}
	}

	// 2. Fetch resources
	assets, err := s.netWorthRepo.GetAssetsByUser(user.ID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch assets: %w", err)
	}

	liabilities, err := s.netWorthRepo.GetLiabilitiesByUser(user.ID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch liabilities: %w", err)
	}

	activeDebts, err := s.debtRepo.GetActiveDebtsByUser(user.ID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch active debts: %w", err)
	}

	allTxs, err := s.txRepo.GetByUserID(user.ID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch user transactions: %w", err)
	}

	// 3. Compute available_balance (sum of cash, bank, ewallet)
	var availableBalance int64
	for _, a := range assets {
		t := strings.ToLower(a.AssetType)
		if t == "cash" || t == "bank" || t == "ewallet" {
			availableBalance += a.Amount
		}
	}

	// 4. Compute daily_burn_rate (average daily expense from last 14 days)
	now := time.Now().In(s.loc).Truncate(24 * time.Hour)
	fourteenDaysAgo := now.AddDate(0, 0, -14)

	var totalExpenseLast14Days int64
	for _, tx := range allTxs {
		// Only check expenses in the last 14 days (up to end of today)
		if tx.Type == "expense" && !tx.TransactionDate.Before(fourteenDaysAgo) && tx.TransactionDate.Before(now.AddDate(0, 0, 1)) {
			totalExpenseLast14Days += tx.Amount
		}
	}
	dailyBurnRate := totalExpenseLast14Days / 14

	// 5. Calculate days until target
	target := targetDate.In(s.loc).Truncate(24 * time.Hour)
	daysUntilTarget := int64(target.Sub(now).Hours() / 24)
	if daysUntilTarget < 0 {
		daysUntilTarget = 0
	}

	projectedExpense := dailyBurnRate * daysUntilTarget

	// 6. Compute upcoming_obligations = upcoming payable debts + recurring expenses + liabilities due before target date
	// (a) upcoming payable debts (due before or on targetDate, and not in the past)
	var upcomingPayableDebts int64
	for _, d := range activeDebts {
		if d.Direction == "payable" {
			remaining := d.Amount - d.PaidAmount
			if remaining > 0 {
				if d.DueDate == nil || d.DueDate.Before(targetDate) || d.DueDate.Equal(targetDate) {
					upcomingPayableDebts += remaining
				}
			}
		}
	}

	// (b) recurring expenses (detected subscriptions due before or on targetDate)
	subs := detectSubscriptions(allTxs)
	var recurringObligations int64
	for _, sub := range subs {
		if sub.NextDate.Before(targetDate) || sub.NextDate.Equal(targetDate) {
			recurringObligations += sub.Amount
		}
	}

	// (c) liabilities due before or on targetDate
	var liabilitiesObligations int64
	for _, l := range liabilities {
		if l.DueDate != nil && (l.DueDate.Before(targetDate) || l.DueDate.Equal(targetDate)) {
			liabilitiesObligations += l.Amount
		}
	}

	upcomingObligations := upcomingPayableDebts + recurringObligations + liabilitiesObligations

	// 7. Compute projected_balance
	projectedBalance := availableBalance - projectedExpense - upcomingObligations

	// 8. Determine risk level
	// safe if projected_balance > 20% of available_balance
	// risky if projected_balance > 0
	// deficit if projected_balance <= 0
	var riskLevel string
	if projectedBalance <= 0 {
		riskLevel = "deficit"
	} else if availableBalance > 0 && projectedBalance > availableBalance/5 {
		riskLevel = "safe"
	} else {
		riskLevel = "risky"
	}

	// 9. Generate insight
	var statusLabel, insight string
	targetDateStr := fmt.Sprintf("%d %s %d", target.Day(), translateMonthID(target.Month()), target.Year())

	switch riskLevel {
	case "safe":
		statusLabel = "🟢 *Aman*"
		insight = fmt.Sprintf("Keuangan kamu diproyeksikan aman sampai tanggal %s. Saldo proyeksi masih di atas 20%% dari saldo saat ini. Pertahankan gaya hidup hematmu!", targetDateStr)
	case "risky":
		statusLabel = "🟡 *Cukup Berisiko*"
		insight = fmt.Sprintf("Keuangan kamu agak riskan menjelang tanggal %s. Proyeksi saldo akhir tersisa tipis. Disarankan untuk membatasi pengeluaran non-primer agar tidak defisit.", targetDateStr)
	case "deficit":
		statusLabel = "🔴 *Defisit/Kritis*"
		insight = fmt.Sprintf("Waspada! Proyeksi saldo kamu diprediksi minus %s pada tanggal %s. Segera kurangi pengeluaran harian atau tunda pembayaran kewajiban non-mendesak jika memungkinkan!", formatIDRCurrency(-projectedBalance), targetDateStr)
	}

	if len(allTxs) == 0 {
		insight = fmt.Sprintf("Insight belum maksimal karena kamu belum memiliki riwayat transaksi pengeluaran untuk menghitung burn rate secara akurat. %s", insight)
	}

	// 10. Save prediction snapshot to DB
	prediction := &models.CashflowPrediction{
		UserID:              user.ID,
		StartDate:           now,
		TargetDate:          target,
		AvailableBalance:    availableBalance,
		DailyBurnRate:       dailyBurnRate,
		ProjectedExpense:    projectedExpense,
		UpcomingObligations: upcomingObligations,
		ProjectedBalance:    projectedBalance,
		RiskLevel:           riskLevel,
		Insight:             insight,
	}

	err = s.cashflowRepo.CreatePrediction(prediction)
	if err != nil {
		return nil, "", fmt.Errorf("failed to save prediction: %w", err)
	}

	// 11. Format deterministic output message in Indonesian Rupiah
	var msgText strings.Builder
	msgText.WriteString("🔮 *Proyeksi Cashflow*\n\n")
	msgText.WriteString(fmt.Sprintf("• *Periode:* %s s/d %s (%d hari)\n",
		now.Format("02/01/2006"), target.Format("02/01/2006"), daysUntilTarget))
	msgText.WriteString(fmt.Sprintf("• *Saldo Kas (Cash/Bank/E-Wallet):* %s\n", formatIDRCurrency(availableBalance)))
	msgText.WriteString(fmt.Sprintf("• *Burn Rate Harian:* %s / hari\n", formatIDRCurrency(dailyBurnRate)))
	msgText.WriteString(fmt.Sprintf("• *Proyeksi Pengeluaran (%d hari):* %s\n", daysUntilTarget, formatIDRCurrency(projectedExpense)))
	msgText.WriteString(fmt.Sprintf("• *Kewajiban Mendatang:* %s\n", formatIDRCurrency(upcomingObligations)))
	msgText.WriteString(fmt.Sprintf("• *Proyeksi Saldo Akhir:* %s\n\n", formatIDRCurrency(projectedBalance)))
	msgText.WriteString(fmt.Sprintf("• *Status Risiko:* %s\n\n", statusLabel))

	// 12. Enrich with AI insight (best-effort; falls back to deterministic insight)
	aiInsight := s.fetchCashflowAIInsight(prediction, target, allTxs)
	msgText.WriteString("💡 *Analisis AI:*\n")
	msgText.WriteString(aiInsight)

	return prediction, msgText.String(), nil
}

// fetchCashflowAIInsight calls the Python AI service and formats the response.
// It always returns a non-empty string — falls back to the deterministic insight
// if the AI service is unavailable or returns an error.
func (s *financeService) fetchCashflowAIInsight(p *models.CashflowPrediction, target time.Time, allTxs []*models.Transaction) string {
	if s.cashflowAI == nil {
		return p.Insight
	}

	// Collect top spending categories from transactions (last 14 days)
	now := time.Now().In(s.loc).Truncate(24 * time.Hour)
	fourteenDaysAgo := now.AddDate(0, 0, -14)
	catTotals := make(map[string]int64)
	for _, tx := range allTxs {
		if tx.Type == "expense" && !tx.TransactionDate.Before(fourteenDaysAgo) && tx.TransactionDate.Before(now.AddDate(0, 0, 1)) {
			catTotals[tx.Category] += tx.Amount
		}
	}
	type catEntry struct {
		name string
		total int64
	}
	var cats []catEntry
	for cat, total := range catTotals {
		cats = append(cats, catEntry{cat, total})
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i].total > cats[j].total })
	var topCategories []string
	for i, c := range cats {
		if i >= 3 {
			break
		}
		topCategories = append(topCategories, c.name)
	}

	req := CashflowInsightRequest{
		AvailableBalance:    p.AvailableBalance,
		DailyBurnRate:       p.DailyBurnRate,
		ProjectedExpense:    p.ProjectedExpense,
		UpcomingObligations: p.UpcomingObligations,
		ProjectedBalance:    p.ProjectedBalance,
		RiskLevel:           p.RiskLevel,
		TargetDate:          target.Format("2006-01-02"),
		TopCategories:       topCategories,
	}

	insight, err := s.cashflowAI.AnalyzeCashflow(req)
	if err != nil {
		log.Printf("[PredictCashflow] AI insight failed (using fallback): %v", err)
		return p.Insight
	}

	// Build formatted AI insight block
	var sb strings.Builder
	sb.WriteString(insight.Summary)
	if len(insight.Recommendations) > 0 {
		sb.WriteString("\n\n📌 *Rekomendasi:*\n")
		for i, rec := range insight.Recommendations {
			// Cap at 3 recommendations to stay within Telegram message limit
			if i >= 3 {
				break
			}
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, rec))
		}
	}
	return sb.String()
}

// AnalyzeCashflowInsight is a pass-through for the CashflowAIClient,
// exposed on the FinanceService interface for direct use (e.g. testing).
func (s *financeService) AnalyzeCashflowInsight(req CashflowInsightRequest) (*CashflowInsightResponse, error) {
	if s.cashflowAI == nil {
		return nil, fmt.Errorf("cashflow AI client not configured")
	}
	return s.cashflowAI.AnalyzeCashflow(req)
}

func detectSubscriptions(txs []*models.Transaction) []Subscription {
	groups := make(map[string][]*models.Transaction)
	for _, tx := range txs {
		if tx.Type != "expense" {
			continue
		}
		desc := strings.ToLower(strings.TrimSpace(tx.Description))
		if desc == "" {
			continue
		}
		groups[desc] = append(groups[desc], tx)
	}

	var subs []Subscription
	for desc, list := range groups {
		if len(list) < 2 {
			continue
		}

		sort.Slice(list, func(i, j int) bool {
			return list[i].TransactionDate.Before(list[j].TransactionDate)
		})

		var intervals []int
		for i := 1; i < len(list); i++ {
			diff := list[i].TransactionDate.Sub(list[i-1].TransactionDate)
			days := int(math.Round(diff.Hours() / 24))
			intervals = append(intervals, days)
		}

		isWeekly := true
		isMonthly := true
		isYearly := true

		for _, days := range intervals {
			if days < 5 || days > 9 {
				isWeekly = false
			}
			if days < 25 || days > 35 {
				isMonthly = false
			}
			if days < 350 || days > 380 {
				isYearly = false
			}
		}

		var intervalType string
		var nextInterval time.Duration

		if isWeekly {
			intervalType = "Mingguan"
			nextInterval = 7 * 24 * time.Hour
		} else if isMonthly {
			intervalType = "Bulanan"
			nextInterval = 30 * 24 * time.Hour
		} else if isYearly {
			intervalType = "Tahunan"
			nextInterval = 365 * 24 * time.Hour
		} else {
			sum := 0
			for _, days := range intervals {
				sum += days
			}
			avgDays := float64(sum) / float64(len(intervals))
			consistent := true
			for _, days := range intervals {
				if math.Abs(float64(days)-avgDays) > 4 {
					consistent = false
					break
				}
			}

			if consistent {
				if avgDays >= 6 && avgDays <= 8 {
					intervalType = "Mingguan"
					nextInterval = 7 * 24 * time.Hour
				} else if avgDays >= 25 && avgDays <= 35 {
					intervalType = "Bulanan"
					nextInterval = 30 * 24 * time.Hour
				} else if avgDays >= 350 && avgDays <= 380 {
					intervalType = "Tahunan"
					nextInterval = 365 * 24 * time.Hour
				}
			}
		}

		if intervalType != "" {
			latestTx := list[len(list)-1]
			var nextDate time.Time
			if intervalType == "Bulanan" {
				nextDate = latestTx.TransactionDate.AddDate(0, 1, 0)
			} else if intervalType == "Tahunan" {
				nextDate = latestTx.TransactionDate.AddDate(1, 0, 0)
			} else if intervalType == "Mingguan" {
				nextDate = latestTx.TransactionDate.AddDate(0, 0, 7)
			} else {
				nextDate = latestTx.TransactionDate.Add(nextInterval)
			}

			subs = append(subs, Subscription{
				Description: desc,
				Amount:      latestTx.Amount,
				Interval:    intervalType,
				LastDate:    latestTx.TransactionDate,
				NextDate:    nextDate,
				Occurrences: len(list),
			})
		}
	}
	return subs
}

func translateMonthID(m time.Month) string {
	months := map[time.Month]string{
		time.January:   "Januari",
		time.February:  "Februari",
		time.March:     "Maret",
		time.April:     "April",
		time.May:       "Mei",
		time.June:      "Juni",
		time.July:      "Juli",
		time.August:    "Agustus",
		time.September: "September",
		time.October:   "Oktober",
		time.November:  "November",
		time.December:  "Desember",
	}
	return months[m]
}
