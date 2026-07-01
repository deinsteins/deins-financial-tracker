package services

import (
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"finance-bot/bot/models"
	"finance-bot/bot/repositories"
	"finance-bot/bot/llm"
)

type FinanceService interface {
	RegisterUser(telegramID int64, name string) (*models.User, error)
	AddTransaction(telegramID int64, txType, category string, amount int64, desc string, walletName string) (*models.Transaction, error)
	GetTodaySummary(telegramID int64) (string, error)
	GetMonthSummary(telegramID int64) (string, error)
	AnalyzeText(telegramID int64, text string) (string, error)
	GenerateAIAnalysis(telegramID int64) (string, error)
	GetTransactions(telegramID int64, limit int, txType string) ([]*models.Transaction, error)
	SetMonthlyBudget(telegramID int64, amount int64) (string, error)
	SetCategoryBudget(telegramID int64, category string, amount int64) (string, error)
	GetBudgetStatus(telegramID int64) (string, error)
	CheckBudgetAlerts(telegramID int64, category string) (string, error)
	AddGoal(telegramID int64, name string, targetAmount int64, deadline time.Time) (string, error)
	GetGoalStatus(telegramID int64) (string, error)
	GetWalletBalances(telegramID int64) (string, error)
	GetSubscriptions(telegramID int64) (string, error)
	GetChatHistory(telegramID int64) ([]llm.Message, error)
	SaveChatHistory(telegramID int64, role, content string) error
	OCRReceipt(fileData []byte, filename string) (*OCRReceiptResponse, error)
	AddDebt(telegramID int64, personName, direction string, amount int64, description string, dueDate *time.Time) (*models.Debt, error)
	GetDebts(telegramID int64, activeOnly bool) ([]*models.Debt, error)
	GetDebtsByPersonName(telegramID int64, personName string) ([]*models.Debt, error)
	PayDebt(telegramID int64, debtID string, amount int64, note string) (*models.DebtPayment, *models.Debt, error)
	SettleDebt(telegramID int64, debtID string) error
	CancelDebt(telegramID int64, debtID string) error
	GetDebtSummary(telegramID int64) (string, error)
	ParseDebtText(text string) (*DebtParseResponse, error)
}

type financeService struct {
	ai             AIClient
	debtAI         DebtAIClient
	userRepo       repositories.UserRepository
	txRepo         repositories.TransactionRepository
	repRepo        repositories.ReportRepository
	budgetRepo     repositories.BudgetRepository
	goalRepo       repositories.GoalRepository
	walletRepo     repositories.WalletRepository
	chatMemoryRepo repositories.ChatMemoryRepository
	debtRepo       repositories.DebtRepository
	loc            *time.Location
}

func NewFinanceService(
	ai AIClient,
	debtAI DebtAIClient,
	userRepo repositories.UserRepository,
	txRepo repositories.TransactionRepository,
	repRepo repositories.ReportRepository,
	budgetRepo repositories.BudgetRepository,
	goalRepo repositories.GoalRepository,
	walletRepo repositories.WalletRepository,
	chatMemoryRepo repositories.ChatMemoryRepository,
	debtRepo repositories.DebtRepository,
) FinanceService {
	tz := os.Getenv("TZ")
	if tz == "" {
		tz = "Asia/Jakarta"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}

	return &financeService{
		ai:             ai,
		debtAI:         debtAI,
		userRepo:       userRepo,
		txRepo:         txRepo,
		repRepo:        repRepo,
		budgetRepo:     budgetRepo,
		goalRepo:       goalRepo,
		walletRepo:     walletRepo,
		chatMemoryRepo: chatMemoryRepo,
		debtRepo:       debtRepo,
		loc:            loc,
	}
}

// getOrCreateUser checks if user exists, if not creates them
func (s *financeService) getOrCreateUser(telegramID int64, name string) (*models.User, error) {
	user, err := s.userRepo.GetByTelegramID(telegramID)
	if err != nil {
		return nil, fmt.Errorf("error finding user: %w", err)
	}

	if user != nil {
		return user, nil
	}

	log.Printf("[FinanceService] User %d not found in DB. Auto-creating...", telegramID)
	newUser := &models.User{
		TelegramID:    telegramID,
		FullName:      name,
		MonthlyBudget: 0,
	}

	err = s.userRepo.Create(newUser)
	if err != nil {
		return nil, fmt.Errorf("error creating user: %w", err)
	}

	// Create default wallets
	err = s.walletRepo.CreateDefaultWallets(newUser.ID)
	if err != nil {
		log.Printf("ERROR: failed to create default wallets: %v", err)
	}

	return newUser, nil
}

func (s *financeService) RegisterUser(telegramID int64, name string) (*models.User, error) {
	return s.getOrCreateUser(telegramID, name)
}

func (s *financeService) AddTransaction(telegramID int64, txType, category string, amount int64, desc string, walletName string) (*models.Transaction, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return nil, err
	}

	walletName = strings.TrimSpace(walletName)
	if walletName == "" {
		walletName = "cash"
	}

	wallet, err := s.walletRepo.EnsureWallet(user.ID, walletName)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure wallet '%s': %w", walletName, err)
	}

	tx := &models.Transaction{
		UserID:      user.ID,
		Type:        txType,
		Category:    category,
		Amount:      amount,
		Description: desc,
		WalletID:    &wallet.ID,
	}

	err = s.txRepo.Create(tx)
	if err != nil {
		return nil, err
	}

	// Update/Deduct balance
	balanceOffset := amount
	if txType == "expense" {
		balanceOffset = -amount
	}
	err = s.walletRepo.UpdateBalance(wallet.ID, balanceOffset)
	if err != nil {
		log.Printf("ERROR: failed to update wallet balance: %v", err)
	}

	return tx, nil
}

func (s *financeService) GetTodaySummary(telegramID int64) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	txs, err := s.txRepo.GetToday(user.ID, s.loc.String())
	if err != nil {
		return "", fmt.Errorf("failed to fetch today's transactions: %w", err)
	}

	title := fmt.Sprintf("📅 *Rekap Keuangan Hari Ini* (%s)", time.Now().In(s.loc).Format("02 Jan 2006"))
	return s.buildFinanceSummary(title, txs), nil
}

func (s *financeService) GetMonthSummary(telegramID int64) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	txs, err := s.txRepo.GetMonth(user.ID, s.loc.String())
	if err != nil {
		return "", fmt.Errorf("failed to fetch monthly transactions: %w", err)
	}

	title := fmt.Sprintf("🗓️ *Rekap Keuangan Bulan Ini* (%s)", time.Now().In(s.loc).Format("Jan 2006"))
	return s.buildFinanceSummary(title, txs), nil
}

func (s *financeService) buildFinanceSummary(title string, txs []*models.Transaction) string {
	if len(txs) == 0 {
		return fmt.Sprintf("%s\n\n💰 *Total Pemasukan*: Rp 0\n💸 *Total Pengeluaran*: Rp 0\n\n📂 *Belum ada catatan transaksi sama sekali nih bro.*", title)
	}

	var totalIncome int64
	var totalExpense int64
	categoryExpenses := make(map[string]int64)
	categoryIncomes := make(map[string]int64)

	for _, tx := range txs {
		if tx.Type == "income" {
			totalIncome += tx.Amount
			categoryIncomes[tx.Category] += tx.Amount
		} else {
			totalExpense += tx.Amount
			categoryExpenses[tx.Category] += tx.Amount
		}
	}

	breakdownStr := ""
	if len(categoryExpenses) > 0 {
		breakdownStr += "\n*Pengeluaran Berdasarkan Kategori*:\n"
		for cat, amt := range categoryExpenses {
			breakdownStr += fmt.Sprintf("• *%s*: %s\n", cat, formatIDRCurrency(amt))
		}
	}
	if len(categoryIncomes) > 0 {
		breakdownStr += "\n*Pemasukan Berdasarkan Kategori*:\n"
		for cat, amt := range categoryIncomes {
			breakdownStr += fmt.Sprintf("• *%s*: %s\n", cat, formatIDRCurrency(amt))
		}
	}

	var biggestCategory string
	var biggestAmount int64
	for cat, amt := range categoryExpenses {
		if amt > biggestAmount {
			biggestAmount = amt
			biggestCategory = cat
		}
	}

	biggestSpendingStr := ""
	if biggestCategory != "" {
		biggestSpendingStr = fmt.Sprintf("\n🔥 *Pengeluaran Terbanyak Lu*:\n• *%s*: %s\n", biggestCategory, formatIDRCurrency(biggestAmount))
	}

	summaryText := fmt.Sprintf(
		"%s\n\n"+
			"💰 *Total Pemasukan*: %s\n"+
			"💸 *Total Pengeluaran*: %s\n"+
			"---"+
			"%s"+
			"%s",
		title,
		formatIDRCurrency(totalIncome),
		formatIDRCurrency(totalExpense),
		breakdownStr,
		biggestSpendingStr,
	)

	return summaryText
}

func (s *financeService) AnalyzeText(telegramID int64, text string) (string, error) {
	// 1. Get or register user
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	log.Printf("[FinanceService] Querying AI service to parse text: '%s'", text)
	
	// 2. Parse text via AI client
	parsed, err := s.ai.ParseTransaction(text)
	if err != nil {
		log.Printf("ERROR: AI parsing failed: %v", err)
		return "", fmt.Errorf("Aduh, gagal nyatet transaksinya nih bro. Info error: %v", err)
	}

	// 3. Save transaction to database
	tx := &models.Transaction{
		UserID:      user.ID,
		Type:        parsed.Type,
		Category:    parsed.Category,
		Amount:      parsed.Amount,
		Description: parsed.Description,
	}

	err = s.txRepo.Create(tx)
	if err != nil {
		log.Printf("ERROR: Failed to save transaction: %v", err)
		return "", fmt.Errorf("berhasil diparsing AI, tapi gagal disimpan ke database: %w", err)
	}

	// 4. Format return card
	typeEmoji := "💸 pengeluaran"
	if parsed.Type == "income" {
		typeEmoji = "💰 pemasukan"
	}

	formattedAmount := formatIDRCurrency(parsed.Amount)

	result := fmt.Sprintf(
		"✅ *Catatan Berhasil Disimpan!* 🎉\n\n"+
			"• *Tipe*: %s\n"+
			"• *Kategori*: %s\n"+
			"• *Jumlah*: %s\n"+
			"• *Deskripsi*: %s\n\n"+
			"💾 _Udah masuk database aman ya bro._",
		typeEmoji,
		parsed.Category,
		formattedAmount,
		parsed.Description,
	)

	return result, nil
}

func formatIDRCurrency(amount int64) string {
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
		return "-Rp " + res
	}
	return "Rp " + res
}

func (s *financeService) GenerateAIAnalysis(telegramID int64) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	txs, err := s.txRepo.GetMonth(user.ID, s.loc.String())
	if err != nil {
		return "", fmt.Errorf("failed to fetch transactions for analysis: %w", err)
	}

	if len(txs) == 0 {
		return "🗓️ *Bulan ini lu belum nyatet transaksi sama sekali nih bro.* Yuk catat dulu transaksi lu, contoh: 'beli kopi 25rb'!", nil
	}

	log.Printf("[FinanceService] Requesting AI analysis for user %s with %d transactions", user.ID, len(txs))
	analysis, err := s.ai.AnalyzeTransactions(txs)
	if err != nil {
		return "", fmt.Errorf("AI Analisis gagal nih bro: %w", err)
	}

	anomaliesStr := "• _Tidak terdeteksi anomali pengeluaran_\n"
	if len(analysis.Anomalies) > 0 {
		anomaliesStr = ""
		for _, item := range analysis.Anomalies {
			anomaliesStr += fmt.Sprintf("• %s\n", item)
		}
	}

	wastefulStr := "• _Tidak terdeteksi pemborosan_\n"
	if len(analysis.WastefulSpending) > 0 {
		wastefulStr = ""
		for _, item := range analysis.WastefulSpending {
			wastefulStr += fmt.Sprintf("• %s\n", item)
		}
	}

	trendsStr := "• _Belum ada data tren_\n"
	if len(analysis.Trends) > 0 {
		trendsStr = ""
		for _, item := range analysis.Trends {
			trendsStr += fmt.Sprintf("• %s\n", item)
		}
	}

	recsStr := ""
	for _, item := range analysis.SavingRecommendations {
		recsStr += fmt.Sprintf("• %s\n", item)
	}
	if recsStr == "" {
		recsStr = "• _Tetap catat keuangan rutin untuk tips lainnya_\n"
	}

	formattedResponse := fmt.Sprintf(
		"🤖 *Hasil Analisis Keuangan AI* 📊\n\n"+
			"🏆 *Skor Keuangan*: `%d/100`\n\n"+
			"*Ringkasan Keuangan Lu*:\n%s\n\n"+
			"📅 *Hari Pengeluaran Tertinggi*:\n• %s\n\n"+
			"🚨 *Deteksi Anomali Belanja*:\n%s\n"+
			"💸 *Bocor Alus (Pemborosan)*:\n%s\n"+
			"📈 *Tren Pengeluaran Kategori*:\n%s\n"+
			"💡 *Saran Penghematan & Tips*:\n%s",
		analysis.FinancialScore,
		analysis.Summary,
		analysis.HighestSpendingDay,
		anomaliesStr,
		wastefulStr,
		trendsStr,
		recsStr,
	)

	return formattedResponse, nil
}

func (s *financeService) GetTransactions(telegramID int64, limit int, txType string) ([]*models.Transaction, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return nil, err
	}

	txs, err := s.txRepo.GetByUserID(user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transactions: %w", err)
	}

	// Filter and limit
	var filtered []*models.Transaction
	for _, tx := range txs {
		if txType != "" && tx.Type != txType {
			continue
		}
		filtered = append(filtered, tx)
		if len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

func (s *financeService) SetMonthlyBudget(telegramID int64, amount int64) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	err = s.userRepo.UpdateBudget(user.ID, amount)
	if err != nil {
		return "", fmt.Errorf("gagal update budget bulanan: %w", err)
	}

	return fmt.Sprintf("✅ *Budget Bulanan Berhasil Diupdate!* 💰\n\nLimit spending bulanan lu sekarang set ke *%s*.", formatIDRCurrency(amount)), nil
}

func (s *financeService) SetCategoryBudget(telegramID int64, category string, amount int64) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	err = s.budgetRepo.SetLimit(user.ID, category, amount)
	if err != nil {
		return "", fmt.Errorf("gagal update budget kategori: %w", err)
	}

	return fmt.Sprintf("✅ *Budget Kategori Berhasil Diupdate!* 💰\n\nLimit spending kategori *%s* lu sekarang set ke *%s*.", category, formatIDRCurrency(amount)), nil
}

func (s *financeService) GetBudgetStatus(telegramID int64) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	// 1. Get all category budgets
	limits, err := s.budgetRepo.GetLimits(user.ID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch budgets: %w", err)
	}

	// 2. Fetch all transactions this month to calculate spending
	txs, err := s.txRepo.GetMonth(user.ID, s.loc.String())
	if err != nil {
		return "", fmt.Errorf("failed to fetch monthly transactions: %w", err)
	}

	var totalExpense int64
	categoryExpenses := make(map[string]int64)
	for _, tx := range txs {
		if tx.Type == "expense" {
			totalExpense += tx.Amount
			categoryExpenses[tx.Category] += tx.Amount
		}
	}

	// 3. Format budget list
	var statusMsg strings.Builder
	statusMsg.WriteString("📊 *Status Budget Bulan Ini*\n\n")

	if len(limits) == 0 && user.MonthlyBudget == 0 {
		statusMsg.WriteString("📂 *Belum ada budget yang disetel nih bro.*\nGunakan perintah `/budget set <kategori> <jumlah>` untuk menyetel budget.")
		return statusMsg.String(), nil
	}

	// Category Budgets
	if len(limits) > 0 {
		statusMsg.WriteString("*Budget Kategori*:\n")
		for cat, limit := range limits {
			spent := categoryExpenses[cat]
			pct := 0.0
			if limit > 0 {
				pct = (float64(spent) / float64(limit)) * 100
			}

			statusIndicator := "✅ *Aman*"
			if pct > 100 {
				statusIndicator = "🚨 *Over-budget!*"
			} else if pct >= 80 {
				statusIndicator = "⚠️ *Mendekati limit!*"
			}

			statusMsg.WriteString(fmt.Sprintf("• *%s*: %s / %s (%.1f%%) %s\n",
				cat, formatIDRCurrency(spent), formatIDRCurrency(limit), pct, statusIndicator))
		}
		statusMsg.WriteString("\n")
	}

	// Overall Monthly Budget
	if user.MonthlyBudget > 0 {
		pct := 0.0
		pct = (float64(totalExpense) / float64(user.MonthlyBudget)) * 100

		statusIndicator := "✅ *Aman*"
		if pct > 100 {
			statusIndicator = "🚨 *Over-budget!*"
		} else if pct >= 80 {
			statusIndicator = "⚠️ *Mendekati limit!*"
		}

		statusMsg.WriteString(fmt.Sprintf("*Total Pengeluaran Bulanan*:\n• *Limit*: %s / %s (%.1f%%) %s\n",
			formatIDRCurrency(totalExpense), formatIDRCurrency(user.MonthlyBudget), pct, statusIndicator))
	}

	return statusMsg.String(), nil
}

func (s *financeService) CheckBudgetAlerts(telegramID int64, category string) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	// Fetch all transactions this month to calculate spending
	txs, err := s.txRepo.GetMonth(user.ID, s.loc.String())
	if err != nil {
		return "", fmt.Errorf("failed to fetch monthly transactions for budget checking: %w", err)
	}

	var monthlyExpense int64
	var categoryExpense int64
	for _, tx := range txs {
		if tx.Type == "expense" {
			monthlyExpense += tx.Amount
			if tx.Category == category {
				categoryExpense += tx.Amount
			}
		}
	}

	var alerts string

	// 1. Check category budget alert
	categoryLimit, err := s.budgetRepo.GetLimit(user.ID, category)
	if err == nil && categoryLimit > 0 {
		pct := (float64(categoryExpense) / float64(categoryLimit)) * 100
		if pct > 100 {
			overage := categoryExpense - categoryLimit
			alerts += fmt.Sprintf("🚨 *OVER-BUDGET KATEGORI!* 💸\nPengeluaran kategori *%s* bulan ini sudah mencapai *%s* (%.1f%%), melebihi budget kategori lu (*%s*) sebesar *%s*!\n\n",
				category, formatIDRCurrency(categoryExpense), pct, formatIDRCurrency(categoryLimit), formatIDRCurrency(overage))
		} else if pct >= 80 {
			alerts += fmt.Sprintf("⚠️ *PERINGATAN BUDGET KATEGORI!* 💸\nPengeluaran kategori *%s* bulan ini sudah mencapai *%s* (%.1f%%), mendekati limit budget kategori lu (*%s*)!\n\n",
				category, formatIDRCurrency(categoryExpense), pct, formatIDRCurrency(categoryLimit))
		}
	}

	// 2. Check monthly budget alert
	if user.MonthlyBudget > 0 {
		pct := (float64(monthlyExpense) / float64(user.MonthlyBudget)) * 100
		if pct > 100 {
			overage := monthlyExpense - user.MonthlyBudget
			alerts += fmt.Sprintf("🚨 *OVER-BUDGET BULANAN!* 💸\nTotal pengeluaran bulan ini sudah mencapai *%s* (%.1f%%), melebihi budget bulanan lu (*%s*) sebesar *%s*!\n\n",
				formatIDRCurrency(monthlyExpense), pct, formatIDRCurrency(user.MonthlyBudget), formatIDRCurrency(overage))
		} else if pct >= 80 {
			alerts += fmt.Sprintf("⚠️ *PERINGATAN BUDGET BULANAN!* 💸\nTotal pengeluaran bulan ini sudah mencapai *%s* (%.1f%%), mendekati limit budget bulanan lu (*%s*)!\n\n",
				formatIDRCurrency(monthlyExpense), pct, formatIDRCurrency(user.MonthlyBudget))
		}
	}

	return alerts, nil
}

func (s *financeService) GetChatHistory(telegramID int64) ([]llm.Message, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return nil, err
	}

	dbMsgs, err := s.chatMemoryRepo.GetLastN(user.ID, 20)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chat messages: %w", err)
	}

	msgs := make([]llm.Message, len(dbMsgs))
	for i, dbMsg := range dbMsgs {
		msgs[i] = llm.Message{
			Role:    dbMsg.Role,
			Content: dbMsg.Content,
		}
	}
	return msgs, nil
}

func (s *financeService) SaveChatHistory(telegramID int64, role, content string) error {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return err
	}

	err = s.chatMemoryRepo.Append(user.ID, role, content)
	if err != nil {
		return fmt.Errorf("failed to append chat message: %w", err)
	}
	return nil
}

func (s *financeService) AddGoal(telegramID int64, name string, targetAmount int64, deadline time.Time) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	goal := &models.Goal{
		UserID:       user.ID,
		Name:         name,
		TargetAmount: targetAmount,
		Deadline:     deadline,
	}

	err = s.goalRepo.Create(goal)
	if err != nil {
		return "", fmt.Errorf("failed to create goal: %w", err)
	}

	formattedAmount := formatIDRCurrency(targetAmount)
	formattedDate := deadline.In(s.loc).Format("02 Jan 2006")
	return fmt.Sprintf("🎯 *Target Keuangan Baru Berhasil Ditambahkan!* 🎉\n\n"+
		"• *Nama*: %s\n"+
		"• *Target*: %s\n"+
		"• *Deadline*: %s\n\n"+
		"💾 _Semangat nabung bro, gua bantu pantau progress-nya!_", name, formattedAmount, formattedDate), nil
}

func (s *financeService) GetGoalStatus(telegramID int64) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	// 1. Fetch all goals
	goals, err := s.goalRepo.GetByUserID(user.ID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch goals: %w", err)
	}

	if len(goals) == 0 {
		return "🎯 *Belum ada target keuangan yang disetel nih bro.*\nGunain perintah `/goal add <nama> <jumlah> <deadline>` untuk menambah target.", nil
	}

	// 2. Fetch total net savings (income - expense)
	netSavings, err := s.txRepo.GetNetSavings(user.ID)
	if err != nil {
		return "", fmt.Errorf("failed to calculate net savings: %w", err)
	}

	var statusMsg strings.Builder
	statusMsg.WriteString("🎯 *Status Target Keuangan Lu*\n\n")

	now := time.Now().In(s.loc)
	remainingNet := netSavings

	for i, goal := range goals {
		// Calculate remaining months
		remainingMonths := (goal.Deadline.Year()-now.Year())*12 + int(goal.Deadline.Month()-now.Month())
		if remainingMonths <= 0 {
			remainingMonths = 1
		}

		// Calculate progress allocated from net savings (waterfall)
		var progress int64
		if remainingNet >= goal.TargetAmount {
			progress = goal.TargetAmount
			remainingNet -= goal.TargetAmount
		} else if remainingNet > 0 {
			progress = remainingNet
			remainingNet = 0
		} else {
			progress = 0
		}

		pct := (float64(progress) / float64(goal.TargetAmount)) * 100

		// Calculate required monthly saving for the remaining target
		neededAmount := goal.TargetAmount - progress
		var monthlySaving int64
		if neededAmount > 0 {
			monthlySaving = neededAmount / int64(remainingMonths)
		}

		status := "⏳ _Sedang berjalan_"
		if progress >= goal.TargetAmount {
			status = "🎉 *Tercapai!*"
		}

		deadlineStr := goal.Deadline.In(s.loc).Format("02 Jan 2006")
		statusMsg.WriteString(fmt.Sprintf("%d. *%s* %s\n"+
			"   • *Progress*: %s / %s (%.1f%%)\n"+
			"   • *Deadline*: %s (%d bulan lagi)\n"+
			"   • *Saran Menabung*: %s / bulan\n\n",
			i+1, goal.Name, status,
			formatIDRCurrency(progress), formatIDRCurrency(goal.TargetAmount), pct,
			deadlineStr, remainingMonths, formatIDRCurrency(monthlySaving)))
	}

	statusMsg.WriteString(fmt.Sprintf("💰 *Total Net Tabungan Saat Ini*: %s", formatIDRCurrency(netSavings)))
	return statusMsg.String(), nil
}

func (s *financeService) GetWalletBalances(telegramID int64) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	wallets, err := s.walletRepo.GetByUserID(user.ID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch wallets: %w", err)
	}

	var msg strings.Builder
	msg.WriteString("👛 *Saldo Dompet Lu*\n\n")
	for _, w := range wallets {
		msg.WriteString(fmt.Sprintf("• *%s*: %s\n", w.Name, formatIDRCurrency(w.Balance)))
	}
	return msg.String(), nil
}

type Subscription struct {
	Description string
	Amount      int64
	Interval    string // "Mingguan", "Bulanan", "Tahunan"
	LastDate    time.Time
	NextDate    time.Time
	Occurrences int
}

func (s *financeService) GetSubscriptions(telegramID int64) (string, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return "", err
	}

	txs, err := s.txRepo.GetByUserID(user.ID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch transactions: %w", err)
	}

	// 1. Group expenses by normalized description
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

		// Sort transactions by date ascending
		sort.Slice(list, func(i, j int) bool {
			return list[i].TransactionDate.Before(list[j].TransactionDate)
		})

		// Calculate intervals in days
		var intervals []int
		for i := 1; i < len(list); i++ {
			diff := list[i].TransactionDate.Sub(list[i-1].TransactionDate)
			days := int(math.Round(diff.Hours() / 24))
			intervals = append(intervals, days)
		}

		// Check consistency of intervals
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
			// Calculate average interval
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

	// 2. Format the response
	if len(subs) == 0 {
		return "🔄 *Belum mendeteksi adanya pengeluaran berulang atau langganan rutin nih bro.*\n" +
			"Gua bakal otomatis ngedeteksi langganan (seperti Netflix, Spotify, gym) kalau ada transaksi sejenis minimal 2 kali dengan interval waktu yang konsisten.", nil
	}

	// Sort subscriptions by NextDate ascending (earliest due first)
	sort.Slice(subs, func(i, j int) bool {
		return subs[i].NextDate.Before(subs[j].NextDate)
	})

	var responseMsg strings.Builder
	responseMsg.WriteString("🔄 *Langganan & Pengeluaran Rutin Terdeteksi*\n\n")
	now := time.Now().In(s.loc)
	for i, sub := range subs {
		lastDateStr := sub.LastDate.In(s.loc).Format("02 Jan 2006")
		nextDateStr := sub.NextDate.In(s.loc).Format("02 Jan 2006")
		
		daysLeft := int(math.Round(sub.NextDate.Sub(now).Hours() / 24))
		var daysLeftStr string
		if daysLeft < 0 {
			daysLeftStr = fmt.Sprintf("lewat %d hari", -daysLeft)
		} else if daysLeft == 0 {
			daysLeftStr = "hari ini!"
		} else {
			daysLeftStr = fmt.Sprintf("%d hari lagi", daysLeft)
		}

		responseMsg.WriteString(fmt.Sprintf("%d. *%s*\n"+
			"   • *Biaya*: %s / %s\n"+
			"   • *Transaksi Terakhir*: %s\n"+
			"   • *Estimasi Berikutnya*: %s (%s)\n"+
			"   • *Jumlah Deteksi*: %d kali\n\n",
			i+1, sub.Description,
			formatIDRCurrency(sub.Amount), strings.ToLower(sub.Interval),
			lastDateStr, nextDateStr, daysLeftStr, sub.Occurrences))
	}

	return responseMsg.String(), nil
}

func (s *financeService) OCRReceipt(fileData []byte, filename string) (*OCRReceiptResponse, error) {
	return s.ai.OCRReceipt(fileData, filename)
}
