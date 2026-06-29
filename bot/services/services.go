package services

import (
	"fmt"
	"log"
	"os"
	"time"

	"finance-bot/bot/models"
	"finance-bot/bot/repositories"
	"finance-bot/bot/llm"
)

type FinanceService interface {
	RegisterUser(telegramID int64, name string) (*models.User, error)
	AddTransaction(telegramID int64, txType, category string, amount int64, desc string) (*models.Transaction, error)
	GetTodaySummary(telegramID int64) (string, error)
	GetMonthSummary(telegramID int64) (string, error)
	AnalyzeText(telegramID int64, text string) (string, error)
	GenerateAIAnalysis(telegramID int64) (string, error)
	GetTransactions(telegramID int64, limit int, txType string) ([]*models.Transaction, error)
	SetMonthlyBudget(telegramID int64, amount int64) (string, error)
	SetCategoryBudget(telegramID int64, category string, amount int64) (string, error)
	CheckBudgetAlerts(telegramID int64, category string) (string, error)
	GetChatHistory(telegramID int64) ([]llm.Message, error)
	SaveChatHistory(telegramID int64, role, content string) error
}

type financeService struct {
	ai            AIClient
	userRepo      repositories.UserRepository
	txRepo        repositories.TransactionRepository
	repRepo       repositories.ReportRepository
	catBudgetRepo repositories.CategoryBudgetRepository
	chatMemoryRepo repositories.ChatMemoryRepository
	loc           *time.Location
}

func NewFinanceService(
	ai AIClient,
	userRepo repositories.UserRepository,
	txRepo repositories.TransactionRepository,
	repRepo repositories.ReportRepository,
	catBudgetRepo repositories.CategoryBudgetRepository,
	chatMemoryRepo repositories.ChatMemoryRepository,
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
		ai:            ai,
		userRepo:      userRepo,
		txRepo:        txRepo,
		repRepo:       repRepo,
		catBudgetRepo: catBudgetRepo,
		chatMemoryRepo: chatMemoryRepo,
		loc:           loc,
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

	return newUser, nil
}

func (s *financeService) RegisterUser(telegramID int64, name string) (*models.User, error) {
	return s.getOrCreateUser(telegramID, name)
}

func (s *financeService) AddTransaction(telegramID int64, txType, category string, amount int64, desc string) (*models.Transaction, error) {
	user, err := s.getOrCreateUser(telegramID, "Telegram User")
	if err != nil {
		return nil, err
	}

	tx := &models.Transaction{
		UserID:      user.ID,
		Type:        txType,
		Category:    category,
		Amount:      amount,
		Description: desc,
	}

	err = s.txRepo.Create(tx)
	if err != nil {
		return nil, err
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
	s := fmt.Sprintf("%d", amount)
	if len(s) <= 3 {
		return "Rp " + s
	}

	var res []byte
	n := 0
	for i := len(s) - 1; i >= 0; i-- {
		if n > 0 && n%3 == 0 {
			res = append([]byte{'.'}, res...)
		}
		res = append([]byte{s[i]}, res...)
		n++
	}
	return "Rp " + string(res)
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

	insightsStr := ""
	for _, insight := range analysis.Insights {
		insightsStr += fmt.Sprintf("• %s\n", insight)
	}

	formattedResponse := fmt.Sprintf(
		"🤖 *Hasil Analisis Keuangan & Tips Keuangan AI*\n\n"+
			"*Ringkasan Keuangan Lu*:\n%s\n\n"+
			"💡 *Observasi Penting & Tips Hemat Buat Lu*:\n%s",
		analysis.Summary,
		insightsStr,
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

	err = s.catBudgetRepo.SetLimit(user.ID, category, amount)
	if err != nil {
		return "", fmt.Errorf("gagal update budget kategori: %w", err)
	}

	return fmt.Sprintf("✅ *Budget Kategori Berhasil Diupdate!* 💰\n\nLimit spending kategori *%s* lu sekarang set ke *%s*.", category, formatIDRCurrency(amount)), nil
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

	// 1. Check monthly budget alert
	if user.MonthlyBudget > 0 && monthlyExpense > user.MonthlyBudget {
		overage := monthlyExpense - user.MonthlyBudget
		alerts += fmt.Sprintf("⚠️ *PERINGATAN BUDGET BULANAN!* 💸\nTotal pengeluaran bulan ini sudah mencapai *%s*, melebihi budget bulanan lu (*%s*) sebesar *%s*!\n\n",
			formatIDRCurrency(monthlyExpense), formatIDRCurrency(user.MonthlyBudget), formatIDRCurrency(overage))
	}

	// 2. Check category budget alert
	categoryLimit, err := s.catBudgetRepo.GetLimit(user.ID, category)
	if err == nil && categoryLimit > 0 && categoryExpense > categoryLimit {
		overage := categoryExpense - categoryLimit
		alerts += fmt.Sprintf("⚠️ *PERINGATAN BUDGET KATEGORI!* 💸\nPengeluaran kategori *%s* bulan ini sudah mencapai *%s*, melebihi budget kategori lu (*%s*) sebesar *%s*!\n\n",
			category, formatIDRCurrency(categoryExpense), formatIDRCurrency(categoryLimit), formatIDRCurrency(overage))
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
