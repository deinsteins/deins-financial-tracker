package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"finance-bot/bot/models"
	"finance-bot/bot/services"
)

type BotHandler struct {
	bot           *tgbotapi.BotAPI
	finance       services.FinanceService
	orchestration services.OrchestrationService
}

func NewBotHandler(
	bot *tgbotapi.BotAPI,
	finance services.FinanceService,
	orchestration services.OrchestrationService,
) *BotHandler {
	return &BotHandler{
		bot:           bot,
		finance:       finance,
		orchestration: orchestration,
	}
}

type PendingReceipt struct {
	ChatID       int64
	TelegramID   int64
	MessageID    int
	Merchant     string
	Items        []services.OCRReceiptItem
	Total        int64
	Date         *string
	RawText      string
	Category     string
	AwaitingEdit bool
	CreatedAt    time.Time
}

var (
	pendingReceipts   = make(map[int64]*PendingReceipt)
	pendingReceiptsMu sync.Mutex
)

const pendingReceiptTTL = 5 * time.Minute

func setPendingReceipt(chatID int64, pr *PendingReceipt) {
	pendingReceiptsMu.Lock()
	defer pendingReceiptsMu.Unlock()

	now := time.Now()
	for id, existing := range pendingReceipts {
		if now.Sub(existing.CreatedAt) > pendingReceiptTTL {
			delete(pendingReceipts, id)
		}
	}

	pendingReceipts[chatID] = pr
}

func getPendingReceipt(chatID int64) *PendingReceipt {
	pendingReceiptsMu.Lock()
	defer pendingReceiptsMu.Unlock()

	pr, ok := pendingReceipts[chatID]
	if !ok {
		return nil
	}
	if time.Since(pr.CreatedAt) > pendingReceiptTTL {
		delete(pendingReceipts, chatID)
		return nil
	}
	return pr
}

func deletePendingReceipt(chatID int64) {
	pendingReceiptsMu.Lock()
	defer pendingReceiptsMu.Unlock()
	delete(pendingReceipts, chatID)
}

var validCategories = map[string]bool{
	"food": true, "groceries": true, "shopping": true, "transport": true,
	"utilities": true, "entertainment": true, "salary": true, "other": true,
}

func sanitizeCategory(cat string) string {
	cat = strings.ToLower(strings.TrimSpace(cat))
	if validCategories[cat] {
		return cat
	}
	return "other"
}

func (h *BotHandler) HandleUpdates(updates tgbotapi.UpdatesChannel) {
	for update := range updates {
		if update.CallbackQuery != nil {
			h.handleCallback(update.CallbackQuery)
			continue
		}

		if update.Message == nil { // Ignore non-message updates
			continue
		}

		// Log incoming message
		log.Printf("[%s] %s (ChatID: %d)", update.Message.From.UserName, update.Message.Text, update.Message.Chat.ID)

		// Handle command, photo, or text message
		if update.Message.IsCommand() {
			h.handleCommand(update.Message)
		} else if update.Message.Photo != nil && len(update.Message.Photo) > 0 {
			h.handlePhotoMessage(update.Message)
		} else {
			h.handleTextMessage(update.Message)
		}
	}
}

func (h *BotHandler) handleCommand(msg *tgbotapi.Message) {
	var replyText string
	var err error

	switch msg.Command() {
	case "start":
		_, err = h.finance.RegisterUser(msg.From.ID, msg.From.FirstName)
		if err != nil {
			log.Printf("Error registering user: %v", err)
		}
		replyText = fmt.Sprintf("Halo %s! Kenalin, gua asisten keuangan pribadi lu nih 😎\n\n"+
			"Lu bisa pakai perintah-perintah ini ya:\n"+
			"/start - Mulai ulang bot & sapa gua\n"+
			"/today - Cek pengeluaran lu hari ini\n"+
			"/month - Cek rekap bulanan lu\n"+
			"/delete - Hapus transaksi lu\n"+
			"/budget set <kategori> <jumlah> - Set budget bulanan kategori tertentu\n"+
			"/budget status - Cek status budget pengeluaran lu\n"+
			"/goal add <nama> <jumlah> <deadline> - Set target keuangan baru\n"+
			"/goal status - Cek progress target keuangan lu\n"+
			"/wallets - Cek saldo dompet (cash, bank, ewallet, etc.)\n"+
			"/debt - Kelola hutang & piutang lu\n"+
			"/subscriptions - Cek daftar pengeluaran rutin/langganan lu\n"+
			"/analyze - Minta AI buatin analisis keuangan lu\n\n"+
			"Atau langsung ketik aja transaksi lu, contoh: 'makan bakso 25rb'. Nanti gua catetin!", msg.From.FirstName)

	case "today":
		replyText, err = h.finance.GetTodaySummary(msg.From.ID)
		if err != nil {
			replyText = "Aduh, gagal ngambil rekap hari ini nih. Coba lagi nanti ya bro!"
		}

	case "month":
		replyText, err = h.finance.GetMonthSummary(msg.From.ID)
		if err != nil {
			replyText = "Waduh, gagal narik rekap bulanan lu. Coba lagi ntar ya bro!"
		}

	case "budget":
		replyText = h.handleBudgetCommand(msg.From.ID, msg.CommandArguments())

	case "goal":
		replyText = h.handleGoalCommand(msg.From.ID, msg.CommandArguments())

	case "wallet", "wallets":
		replyText, err = h.finance.GetWalletBalances(msg.From.ID)
		if err != nil {
			replyText = fmt.Sprintf("⚠️ *Gagal mengambil saldo dompet:*\n%v", err)
		}

	case "subscription", "subscriptions":
		replyText, err = h.finance.GetSubscriptions(msg.From.ID)
		if err != nil {
			replyText = fmt.Sprintf("⚠️ *Gagal mendeteksi langganan:*\n%v", err)
		}

	case "analyze":
		replyText, err = h.finance.GenerateAIAnalysis(msg.From.ID)
		if err != nil {
			replyText = fmt.Sprintf("⚠️ *Error pas analisis keuangan lu nih:*\n%v", err)
		}

	case "debt":
		replyText = h.handleDebtCommand(msg.From.ID, msg.CommandArguments())

	case "delete":
		h.handleDeleteCommand(msg.Chat.ID, msg.From.ID, msg.CommandArguments(), msg.MessageID)
		return

	case "asset":
		replyText = h.handleAssetCommand(msg.From.ID, msg.CommandArguments())

	case "liability":
		replyText = h.handleLiabilityCommand(msg.From.ID, msg.CommandArguments())

	case "networth":
		replyText = h.handleNetWorthCommand(msg.From.ID, msg.CommandArguments())

	case "cashflow":
		replyText = h.handleCashflowCommand(msg.From.ID, msg.CommandArguments())

	case "payday":
		replyText = h.handlePaydayCommand(msg.From.ID, msg.CommandArguments())

	default:
		replyText = "Perintah apaan tuh? Coba cek /start aja biar jelas bro!"
	}

	h.sendReply(msg.Chat.ID, replyText, msg.MessageID)
}

func (h *BotHandler) handleDebtCommand(telegramID int64, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "📋 *Panduan Perintah /debt*:\n\n" +
			"• `/debt add receivable <nama> <jumlah> <deskripsi>`\n  _Catat hutang orang ke lu (piutang)_\n  _Contoh: `/debt add receivable Andi 500rb makan bersama`_\n\n" +
			"• `/debt add payable <nama> <jumlah> <deskripsi>`\n  _Catat hutang lu ke orang lain_\n  _Contoh: `/debt add payable Budi 200rb beli pulsa`_\n\n" +
			"• `/debt list` — Lihat semua hutang & piutang aktif\n" +
			"• `/debt summary` — Lihat ringkasan posisi keuangan hutang lu\n" +
			"• `/debt detail <nama>` — Cek detail hutang per orang\n" +
			"• `/debt history <nama>` — Cek riwayat pembayaran hutang per orang\n" +
			"• `/debt pay <nama> <jumlah>` — Bayar sebagian hutang\n" +
			"• `/debt paid <nama>` — Tandai hutang ke nama itu sudah lunas\n" +
			"• `/debt cancel <nama>` — Batalkan hutang ke nama itu"
	}

	parts := strings.Fields(args)
	subcmd := strings.ToLower(parts[0])

	switch subcmd {
	case "add":
		return h.handleDebtAdd(telegramID, parts[1:])

	case "list":
		return h.handleDebtList(telegramID)

	case "summary":
		return h.handleDebtSummary(telegramID)

	case "detail":
		if len(parts) < 2 {
			return "⚠️ Format salah. Gunakan: `/debt detail <nama>`\n_Contoh: `/debt detail Andi`_"
		}
		personName := strings.Join(parts[1:], " ")
		reply, err := h.finance.GetDebtDetail(telegramID, personName)
		if err != nil {
			return fmt.Sprintf("⚠️ *Gagal mengambil detail hutang:*\n%v", err)
		}
		return reply

	case "history":
		if len(parts) < 2 {
			return "⚠️ Format salah. Gunakan: `/debt history <nama>`\n_Contoh: `/debt history Budi`_"
		}
		personName := strings.Join(parts[1:], " ")
		reply, err := h.finance.GetDebtHistory(telegramID, personName)
		if err != nil {
			return fmt.Sprintf("⚠️ *Gagal mengambil riwayat pembayaran hutang:*\n%v", err)
		}
		return reply

	case "pay":
		return h.handleDebtPay(telegramID, parts[1:])

	case "paid":
		return h.handleDebtPaid(telegramID, parts[1:])

	case "cancel":
		return h.handleDebtCancel(telegramID, parts[1:])

	default:
		return "⚠️ Sub-perintah tidak dikenal.\n\n" +
			"Coba `/debt detail Andi`, `/debt history Budi`, `/debt list`, atau `/debt summary` ya bro!"
	}
}

func (h *BotHandler) handleDebtAdd(telegramID int64, parts []string) string {
	if len(parts) < 4 {
		return "⚠️ Format salah.\n\n" +
			"Gunakan: `/debt add <tipe> <nama> <jumlah> <deskripsi>`\n\n" +
			"*Contoh:*\n" +
			"• `/debt add receivable Andi 500rb makan bersama`\n" +
			"• `/debt add payable Budi 200rb beli pulsa`"
	}

	direction := strings.ToLower(parts[0])
	if direction != "payable" && direction != "receivable" {
		return "⚠️ Tipe harus `receivable` (orang ke lu) atau `payable` (lu ke orang).\n\n" +
			"_Contoh: `/debt add receivable Andi 100rb kopi`_"
	}

	personName := parts[1]
	amount, err := parseAmount(parts[2])
	if err != nil {
		return fmt.Sprintf("⚠️ Jumlah tidak valid: %v\n_Coba: 25rb, 200k, 1.5jt, atau 50000_", err)
	}

	description := strings.Join(parts[3:], " ")

	debt, err := h.finance.AddDebt(telegramID, personName, direction, amount, description, nil)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal menyimpan hutang:*\n%v", err)
	}

	dirLabel := "Hutang ke *%s*"
	if direction == "receivable" {
		dirLabel = "*%s* hutang ke lu"
	}

	return fmt.Sprintf("✅ *Hutang Berhasil Dicatat!* 🎉\n\n"+
		"• *Tipe*: %s\n"+
		"• %s\n"+
		"• *Jumlah*: %s\n"+
		"• *Deskripsi*: %s\n"+
		"• *Status*: %s",
		func() string {
			if direction == "receivable" {
				return "💰 Piutang"
			}
			return "💸 Hutang"
		}(),
		fmt.Sprintf(dirLabel, debt.PersonName),
		formatIDRCurrency(debt.Amount),
		debt.Description,
		"aktif",
	)
}

func (h *BotHandler) handleDebtList(telegramID int64) string {
	debts, err := h.finance.GetDebts(telegramID, true)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mengambil data hutang:*\n%v", err)
	}

	if len(debts) == 0 {
		return "📒 *Daftar Hutang & Piutang*\n\nBelum ada hutang atau piutang aktif nih bro! 🎉"
	}

	var payables, receivables string
	pCount, rCount := 0, 0

	for _, d := range debts {
		remaining := d.Amount - d.PaidAmount
		line := fmt.Sprintf("• *%s*: %s", d.PersonName, formatIDRCurrency(remaining))
		if d.PaidAmount > 0 {
			line += fmt.Sprintf(" _(sudah bayar %s dari %s)_", formatIDRCurrency(d.PaidAmount), formatIDRCurrency(d.Amount))
		}
		if d.DueDate != nil {
			line += fmt.Sprintf("\n  📅 Jatuh tempo: %s", d.DueDate.Format("02 Jan 2006"))
		}
		if d.Description != "" {
			line += fmt.Sprintf("\n  📝 %s", d.Description)
		}
		line += "\n\n"

		if d.Direction == "payable" {
			payables += line
			pCount++
		} else {
			receivables += line
			rCount++
		}
	}

	var sb strings.Builder
	sb.WriteString("📒 *Daftar Hutang & Piutang*\n\n")

	if rCount > 0 {
		sb.WriteString(fmt.Sprintf("💰 *Piutang (%d):*\n%s", rCount, receivables))
	}
	if pCount > 0 {
		sb.WriteString(fmt.Sprintf("💸 *Hutang (%d):*\n%s", pCount, payables))
	}

	sb.WriteString("_Gunakan `/debt pay` atau `/debt paid` untuk membayar._")

	return sb.String()
}

func (h *BotHandler) handleDebtSummary(telegramID int64) string {
	summary, err := h.finance.GetDebtSummary(telegramID)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mengambil ringkasan hutang:*\n%v", err)
	}
	return summary
}

func (h *BotHandler) handleDebtPay(telegramID int64, parts []string) string {
	if len(parts) < 2 {
		return "⚠️ Format salah.\n\n" +
			"Gunakan: `/debt pay <nama> <jumlah>`\n\n" +
			"_Contoh: `/debt pay Andi 100rb`_"
	}

	personName := parts[0]
	amount, err := parseAmount(parts[1])
	if err != nil {
		return fmt.Sprintf("⚠️ Jumlah tidak valid: %v\n_Coba: 25rb, 200k, 1.5jt, atau 50000_", err)
	}

	debts, err := h.finance.GetDebtsByPersonName(telegramID, personName)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mencari hutang:*\n%v", err)
	}
	if len(debts) == 0 {
		return fmt.Sprintf("⚠️ Tidak ditemukan hutang aktif atas nama *%s*.", personName)
	}
	if len(debts) > 1 {
		return fmt.Sprintf("⚠️ Ada %d hutang aktif atas nama *%s*.\nSaat ini gua baru bisa bayar yang paling baru dibuat dulu ya bro!", len(debts), personName)
	}

	debt := debts[0]
	note := fmt.Sprintf("Bayar %s", formatIDRCurrency(amount))

	payment, updatedDebt, err := h.finance.PayDebt(telegramID, debt.ID, amount, note)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal memproses pembayaran:*\n%v", err)
	}

	remaining := updatedDebt.Amount - updatedDebt.PaidAmount

	replyText := fmt.Sprintf("✅ *Pembayaran Berhasil!* 🎉\n\n"+
		"• *Ke:* %s\n"+
		"• *Dibayar:* %s\n"+
		"• *Sisa:* %s\n"+
		"• *Status:* %s",
		debt.PersonName,
		formatIDRCurrency(payment.Amount),
		formatIDRCurrency(remaining),
		func() string {
			if updatedDebt.Status == "paid" {
				return "🟢 Lunas!"
			}
			return "🟡 Belum lunas"
		}(),
	)

	if updatedDebt.Status == "paid" {
		replyText += "\n\n🎉 *Hutang ini sudah lunas!*"
	}

	return replyText
}

func (h *BotHandler) handleDebtPaid(telegramID int64, parts []string) string {
	if len(parts) < 1 {
		return "⚠️ Format salah.\n\n" +
			"Gunakan: `/debt paid <nama>`\n\n" +
			"_Contoh: `/debt paid Andi`_"
	}

	personName := parts[0]

	debts, err := h.finance.GetDebtsByPersonName(telegramID, personName)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mencari hutang:*\n%v", err)
	}
	if len(debts) == 0 {
		return fmt.Sprintf("⚠️ Tidak ditemukan hutang aktif atas nama *%s*.", personName)
	}
	if len(debts) > 1 {
		return fmt.Sprintf("⚠️ Ada %d hutang aktif atas nama *%s*.\nSaat ini gua baru bisa lunasi yang paling baru dibuat dulu ya bro!", len(debts), personName)
	}

	debt := debts[0]

	err = h.finance.SettleDebt(telegramID, debt.ID)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal menandai lunas:*\n%v", err)
	}

	return fmt.Sprintf("✅ *Hutang Lunas!* 🎉\n\n"+
		"• *Nama:* %s\n"+
		"• *Jumlah:* %s\n"+
		"• *Deskripsi:* %s\n\n"+
		"Hutang ini sudah ditandai sebagai *lunas* ya bro! 👍",
		debt.PersonName,
		formatIDRCurrency(debt.Amount),
		debt.Description,
	)
}

func (h *BotHandler) handleDebtCancel(telegramID int64, parts []string) string {
	if len(parts) < 1 {
		return "⚠️ Format salah.\n\n" +
			"Gunakan: `/debt cancel <nama>`\n\n" +
			"_Contoh: `/debt cancel Andi`_"
	}

	personName := parts[0]

	debts, err := h.finance.GetDebtsByPersonName(telegramID, personName)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mencari hutang:*\n%v", err)
	}
	if len(debts) == 0 {
		return fmt.Sprintf("⚠️ Tidak ditemukan hutang aktif atas nama *%s*.", personName)
	}
	if len(debts) > 1 {
		return fmt.Sprintf("⚠️ Ada %d hutang aktif atas nama *%s*.\nSaat ini gua baru bisa batalkan yang paling baru dibuat dulu ya bro!", len(debts), personName)
	}

	debt := debts[0]

	err = h.finance.CancelDebt(telegramID, debt.ID)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal membatalkan hutang:*\n%v", err)
	}

	return fmt.Sprintf("❌ *Hutang Dibatalkan*\n\n"+
		"• *Nama:* %s\n"+
		"• *Jumlah:* %s\n"+
		"• *Deskripsi:* %s\n\n"+
		"Hutang ini sudah dibatalkan ya bro!",
		debt.PersonName,
		formatIDRCurrency(debt.Amount),
		debt.Description,
	)
}

func (h *BotHandler) handleTextMessage(msg *tgbotapi.Message) {
	log.Printf("Parsing text message with Hermes: %s", msg.Text)

	// Intercept custom keyboard buttons
	text := strings.TrimSpace(msg.Text)
	switch text {
	case "📊 Rekap Bulan Ini":
		replyText, err := h.finance.GetMonthSummary(msg.From.ID)
		if err != nil {
			replyText = "Waduh, gagal narik rekap bulanan lu. Coba lagi ntar ya bro!"
		}
		h.sendReply(msg.Chat.ID, replyText, msg.MessageID)
		return
	case "🤖 AI Analisis":
		replyText, err := h.finance.GenerateAIAnalysis(msg.From.ID)
		if err != nil {
			replyText = fmt.Sprintf("⚠️ *Error pas analisis keuangan lu nih:*\n%v", err)
		}
		h.sendReply(msg.Chat.ID, replyText, msg.MessageID)
		return
	case "💵 Cek Dompet":
		replyText, err := h.finance.GetWalletBalances(msg.From.ID)
		if err != nil {
			replyText = fmt.Sprintf("⚠️ *Gagal mengambil saldo dompet:*\n%v", err)
		}
		h.sendReply(msg.Chat.ID, replyText, msg.MessageID)
		return
	case "💰 Cek Net Worth":
		replyText, err := h.finance.GetNetWorthStatus(msg.From.ID)
		if err != nil {
			replyText = fmt.Sprintf("⚠️ *Gagal mengambil status net worth:*\n%v", err)
		}
		h.sendReply(msg.Chat.ID, replyText, msg.MessageID)
		return
	case "🔮 Proyeksi Cashflow":
		replyText := h.handleCashflowCommand(msg.From.ID, "")
		h.sendReply(msg.Chat.ID, replyText, msg.MessageID)
		return
	case "❌ Hapus Terakhir":
		h.handleDeleteCommand(msg.Chat.ID, msg.From.ID, "last", msg.MessageID)
		return
	case "📋 Menu Budget":
		replyText := h.handleBudgetCommand(msg.From.ID, "")
		h.sendReply(msg.Chat.ID, replyText, msg.MessageID)
		return
	case "🤝 Kelola Hutang":
		replyText := h.handleDebtCommand(msg.From.ID, "")
		h.sendReply(msg.Chat.ID, replyText, msg.MessageID)
		return
	}

	if pr := getPendingReceipt(msg.Chat.ID); pr != nil && pr.AwaitingEdit {
		h.handleReceiptAmountEdit(msg, pr)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Extract target wallet if any
	rawText := msg.Text
	words := strings.Fields(rawText)
	var detectedWallet string
	var textToParse = rawText

	if len(words) > 0 {
		firstWord := strings.ToLower(words[0])
		switch firstWord {
		case "cash", "tunai", "dompet":
			detectedWallet = "cash"
		case "bank", "bca", "mandiri", "cimb", "bni", "bri", "jago":
			detectedWallet = firstWord
		case "ewallet", "ovo", "gopay", "dana", "linkaja", "shopeepay":
			detectedWallet = firstWord
		}

		if detectedWallet != "" {
			textToParse = strings.TrimSpace(strings.TrimPrefix(rawText, words[0]))
			ctx = context.WithValue(ctx, "wallet", detectedWallet)
		}
	}

	// Prioritize debt/receivable parsing when the message contains a debt-related
	// keyword. Falls back to normal orchestration below if the AI service call
	// fails or the parsed intent is "unknown" with no identifiable person.
	if h.tryHandleDebtIntent(msg, textToParse) {
		return
	}

	if h.tryHandleNetWorthIntent(msg, textToParse) {
		return
	}

	// 1. Fetch conversational history memory context
	history, err := h.finance.GetChatHistory(msg.From.ID)
	if err != nil {
		log.Printf("WARNING: failed to load chat memory context: %v", err)
		history = nil // fallback to empty context
	}

	intent, err := h.orchestration.ParseIntent(ctx, history, textToParse)
	if err != nil {
		replyText := fmt.Sprintf("⚠️ *Waduh, gagal memproses perintah lewat Hermes:*\n%v", err)
		h.sendReply(msg.Chat.ID, replyText, msg.MessageID)
		return
	}

	// 2. Check if tools need to be executed
	if len(intent.ToolCalls) > 0 {
		var replyText string
		log.Printf("Hermes returned %d tool calls to execute sequentially", len(intent.ToolCalls))

		for i, tc := range intent.ToolCalls {
			log.Printf("Executing tool %d/%d: %s with args: %s", i+1, len(intent.ToolCalls), tc.ToolName, tc.ArgsRaw)
			
			res, err := h.orchestration.Dispatch(ctx, msg.From.ID, tc.ToolName, tc.ArgsRaw)
			if err != nil {
				replyText += fmt.Sprintf("❌ *Gagal mengeksekusi %s:*\n%v\n\n", tc.ToolName, err)
				continue
			}

			// Format response card dynamically based on the tool
			switch tc.ToolName {
			case "save_transaction":
				tx, ok := res.(*models.Transaction)
				if !ok {
					replyText += "✅ *Transaksi Berhasil Disimpan!*\n\n"
					continue
				}
				typeEmoji := "💸 pengeluaran"
				if tx.Type == "income" {
					typeEmoji = "💰 pemasukan"
				}
				walletName := "cash"
				if detectedWallet != "" {
					walletName = detectedWallet
				}
				replyText += fmt.Sprintf("✅ *Catatan Berhasil Disimpan!* 🎉\n\n"+
					"• *Tipe*: %s\n"+
					"• *Kategori*: %s\n"+
					"• *Jumlah*: %s\n"+
					"• *Dompet*: %s\n"+
					"• *Deskripsi*: %s\n\n",
					typeEmoji, tx.Category, formatIDRCurrency(tx.Amount), walletName, tx.Description)

				// Proactively check and append budget alerts
				if tx.Type == "expense" {
					alerts, err := h.finance.CheckBudgetAlerts(msg.From.ID, tx.Category)
					if err == nil && alerts != "" {
						replyText += alerts
					}
				}

			case "get_today_summary":
				if summary, ok := res.(string); ok {
					replyText += summary + "\n\n"
				} else {
					replyText += "📋 *Gagal memformat rekap hari ini.*\n\n"
				}

			case "get_month_summary":
				if summary, ok := res.(string); ok {
					replyText += summary + "\n\n"
				} else {
					replyText += "📋 *Gagal memformat rekap bulanan.*\n\n"
				}

			case "get_transactions":
				txs, ok := res.([]*models.Transaction)
				if !ok {
					replyText += "📋 *Gagal memformat daftar transaksi.*\n\n"
					continue
				}
				if len(txs) == 0 {
					replyText += "📂 *Belum ada riwayat transaksi nih bro.*\n\n"
				} else {
					replyText += "📋 *Daftar Transaksi Terbaru Lu:*\n"
					for _, tx := range txs {
						typeSign := "💸"
						if tx.Type == "income" {
							typeSign = "💰"
						}
						replyText += fmt.Sprintf("• %s *%s*: %s - %s (_%s_)\n",
							typeSign, tx.Category, formatIDRCurrency(tx.Amount), tx.Description, tx.TransactionDate.Format("02 Jan 15:04"))
					}
					replyText += "\n"
				}

			case "analyze_spending":
				if analysis, ok := res.(string); ok {
					replyText += analysis + "\n\n"
				} else {
					replyText += "🤖 *Gagal memformat analisis keuangan.*\n\n"
				}

			case "set_monthly_budget":
				if budgetMsg, ok := res.(string); ok {
					replyText += budgetMsg + "\n\n"
				} else {
					replyText += "✅ *Budget bulanan berhasil diubah.*\n\n"
				}

			case "set_category_budget":
				if budgetMsg, ok := res.(string); ok {
					replyText += budgetMsg + "\n\n"
				} else {
					replyText += "✅ *Budget kategori berhasil diubah.*\n\n"
				}

			case "delete_transaction":
				tx, ok := res.(*models.Transaction)
				if !ok {
					replyText += "✅ *Transaksi Berhasil Dihapus!*\n\n"
					continue
				}
				typeEmoji := "💸 pengeluaran"
				if tx.Type == "income" {
					typeEmoji = "💰 pemasukan"
				}
				replyText += fmt.Sprintf("✅ *Transaksi Berhasil Dihapus!* 🎉\n\n"+
					"• *Tipe*: %s\n"+
					"• *Kategori*: %s\n"+
					"• *Jumlah*: %s\n"+
					"• *Deskripsi*: %s\n\n"+
					"💾 _Saldo dompet dan budget sudah diupdate secara otomatis._\n\n",
					typeEmoji, tx.Category, formatIDRCurrency(tx.Amount), tx.Description)

			default:
				replyText += fmt.Sprintf("✅ *Eksekusi %s berhasil.*\n\n", tc.ToolName)
			}
		}

		h.sendReply(msg.Chat.ID, replyText, msg.MessageID)

		// 3. Save memory context (only for successful turns)
		_ = h.finance.SaveChatHistory(msg.From.ID, "user", msg.Text)
		_ = h.finance.SaveChatHistory(msg.From.ID, "assistant", replyText)
		return
	}

	// 2. Direct conversational fallback
	h.sendReply(msg.Chat.ID, intent.Response, msg.MessageID)

	// 3. Save memory context (only for successful turns)
	_ = h.finance.SaveChatHistory(msg.From.ID, "user", msg.Text)
	_ = h.finance.SaveChatHistory(msg.From.ID, "assistant", intent.Response)
}

func (h *BotHandler) handleReceiptAmountEdit(msg *tgbotapi.Message, pr *PendingReceipt) {
	newAmount, err := parseAmount(msg.Text)
	if err != nil {
		h.sendReply(msg.Chat.ID, fmt.Sprintf("⚠️ Jumlah tidak valid: %v\n_Coba lagi ya bro!_", err), msg.MessageID)
		return
	}

	pr.Total = newAmount
	pr.AwaitingEdit = false
	setPendingReceipt(msg.Chat.ID, pr)

	replyText := formatReceiptSummary(&services.OCRReceiptResponse{
		Merchant: pr.Merchant,
		Items:    pr.Items,
		Total:    pr.Total,
		Date:     pr.Date,
		RawText:  pr.RawText,
	}) + "\n_Simpan sebagai transaksi?_"

	h.sendReplyWithKeyboard(msg.Chat.ID, replyText, msg.MessageID, receiptConfirmKeyboard())

	_ = h.finance.SaveChatHistory(msg.From.ID, "user", msg.Text)
	_ = h.finance.SaveChatHistory(msg.From.ID, "assistant", replyText)
}

func (h *BotHandler) handlePhotoMessage(msg *tgbotapi.Message) {
	log.Printf("Received photo from user %s (ChatID: %d)", msg.From.UserName, msg.Chat.ID)

	h.sendReply(msg.Chat.ID, "🔍 _Lagi gua scan struk lu, tunggu bentar ya..._", msg.MessageID)

	// Pick the highest-resolution photo (last element in the Photo slice)
	photos := msg.Photo
	bestPhoto := photos[len(photos)-1]

	// Get the file download URL from Telegram
	fileConfig := tgbotapi.FileConfig{FileID: bestPhoto.FileID}
	tgFile, err := h.bot.GetFile(fileConfig)
	if err != nil {
		log.Printf("Failed to get file info from Telegram: %v", err)
		h.sendReply(msg.Chat.ID, "⚠️ *Gagal mengambil file foto dari Telegram.*\nCoba kirim ulang ya bro!", msg.MessageID)
		return
	}

	fileURL := tgFile.Link(h.bot.Token)

	// Download the image bytes
	resp, err := http.Get(fileURL)
	if err != nil {
		log.Printf("Failed to download photo from Telegram: %v", err)
		h.sendReply(msg.Chat.ID, "⚠️ *Gagal download foto dari Telegram.*\nCoba kirim ulang ya bro!", msg.MessageID)
		return
	}
	defer resp.Body.Close()

	fileData, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read photo data: %v", err)
		h.sendReply(msg.Chat.ID, "⚠️ *Gagal membaca data foto.*\nCoba kirim ulang ya bro!", msg.MessageID)
		return
	}

	// Derive a filename from the Telegram file path
	filename := tgFile.FilePath
	if idx := strings.LastIndex(filename, "/"); idx >= 0 {
		filename = filename[idx+1:]
	}
	if filename == "" {
		filename = "photo.jpg"
	}

	// Send to FastAPI /ocr endpoint
	ocrResult, err := h.finance.OCRReceipt(fileData, filename)
	if err != nil {
		log.Printf("OCR request failed: %v", err)
		h.sendReply(msg.Chat.ID, fmt.Sprintf("⚠️ *Gagal memproses struk:*\n%v", err), msg.MessageID)
		return
	}

	setPendingReceipt(msg.Chat.ID, &PendingReceipt{
		ChatID:     msg.Chat.ID,
		TelegramID: msg.From.ID,
		MessageID:  msg.MessageID,
		Merchant:   ocrResult.Merchant,
		Items:      ocrResult.Items,
		Total:      ocrResult.Total,
		Date:       ocrResult.Date,
		RawText:    ocrResult.RawText,
		Category:   sanitizeCategory(ocrResult.Category),
		CreatedAt:  time.Now(),
	})

	replyText := formatReceiptSummary(ocrResult) + "\n_Simpan sebagai transaksi?_"
	h.sendReplyWithKeyboard(msg.Chat.ID, replyText, msg.MessageID, receiptConfirmKeyboard())

	_ = h.finance.SaveChatHistory(msg.From.ID, "user", "[Sent a receipt photo]")
	_ = h.finance.SaveChatHistory(msg.From.ID, "assistant", replyText)
}

func formatReceiptSummary(r *services.OCRReceiptResponse) string {
	var sb strings.Builder

	sb.WriteString("🧾 *Hasil Scan Struk*\n\n")

	if r.Merchant != "" {
		sb.WriteString(fmt.Sprintf("🏪 *Toko:* %s\n", r.Merchant))
	}

	if r.Date != nil && *r.Date != "" {
		sb.WriteString(fmt.Sprintf("📅 *Tanggal:* %s\n", *r.Date))
	}

	sb.WriteString("\n")

	if len(r.Items) > 0 {
		sb.WriteString("📋 *Item:*\n")
		for _, item := range r.Items {
			if item.Qty > 1 {
				sb.WriteString(fmt.Sprintf("  • %s ×%d — %s\n", item.Name, item.Qty, formatIDRCurrency(item.Price*int64(item.Qty))))
			} else {
				sb.WriteString(fmt.Sprintf("  • %s — %s\n", item.Name, formatIDRCurrency(item.Price)))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("🏷️ *Kategori:* %s\n", sanitizeCategory(r.Category)))
	sb.WriteString(fmt.Sprintf("💰 *Total:* %s\n", formatIDRCurrency(r.Total)))

	return sb.String()
}

func (h *BotHandler) handleCallback(cq *tgbotapi.CallbackQuery) {
	h.bot.Request(tgbotapi.NewCallback(cq.ID, ""))

	chatID := cq.Message.Chat.ID

	if strings.HasPrefix(cq.Data, "tx:delete:") {
		txID := strings.TrimPrefix(cq.Data, "tx:delete:")
		tx, err := h.finance.DeleteTransaction(cq.From.ID, txID)
		if err != nil {
			log.Printf("Failed to delete transaction: %v", err)
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID,
				fmt.Sprintf("⚠️ *Gagal menghapus transaksi:*\n%v", err))
			edit.ParseMode = "markdown"
			h.bot.Send(edit)
			return
		}

		typeEmoji := "💸 pengeluaran"
		if tx.Type == "income" {
			typeEmoji = "💰 pemasukan"
		}
		successText := fmt.Sprintf("✅ *Transaksi Berhasil Dihapus!* 🎉\n\n"+
			"• *Tipe*: %s\n"+
			"• *Kategori*: %s\n"+
			"• *Jumlah*: %s\n"+
			"• *Deskripsi*: %s\n\n"+
			"💾 _Saldo dompet dan budget sudah diupdate secara otomatis._",
			typeEmoji, tx.Category, formatIDRCurrency(tx.Amount), tx.Description)

		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, successText)
		edit.ParseMode = "markdown"
		h.bot.Send(edit)

		_ = h.finance.SaveChatHistory(cq.From.ID, "assistant", successText)
		return
	}

	pr := getPendingReceipt(chatID)
	if pr == nil {
		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID,
			"⏳ _Struk sudah expired, kirim ulang foto ya bro!_")
		edit.ParseMode = "markdown"
		h.bot.Send(edit)
		return
	}

	switch cq.Data {
	case "ocr:confirm":
		cat := sanitizeCategory(pr.Category)
		desc := formatReceiptDescription(pr)
		tx, err := h.finance.AddTransaction(pr.TelegramID, "expense", cat, pr.Total, desc, "cash")
		if err != nil {
			log.Printf("Failed to save receipt transaction: %v", err)
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID,
				fmt.Sprintf("⚠️ *Gagal menyimpan transaksi:*\n%v", err))
			edit.ParseMode = "markdown"
			h.bot.Send(edit)
			deletePendingReceipt(chatID)
			return
		}

		typeEmoji := "💸 pengeluaran"
		successText := fmt.Sprintf("✅ *Catatan Berhasil Disimpan!* 🎉\n\n"+
			"• *Tipe*: %s\n"+
			"• *Kategori*: %s\n"+
			"• *Jumlah*: %s\n"+
			"• *Dompet*: cash\n"+
			"• *Deskripsi*: %s\n\n",
			typeEmoji, cat, formatIDRCurrency(tx.Amount), desc)

		if alerts, err := h.finance.CheckBudgetAlerts(pr.TelegramID, cat); err == nil && alerts != "" {
			successText += alerts
		}

		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, successText)
		edit.ParseMode = "markdown"
		h.bot.Send(edit)

		_ = h.finance.SaveChatHistory(pr.TelegramID, "assistant", successText)
		deletePendingReceipt(chatID)

	case "ocr:edit":
		pr.AwaitingEdit = true
		setPendingReceipt(chatID, pr)
		h.sendReply(chatID, "✏️ _Ketik jumlah baru (contoh: 25rb, 50000):_", cq.Message.MessageID)

	case "ocr:cancel":
		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "❌ _Struk dibatalkan._")
		edit.ParseMode = "markdown"
		h.bot.Send(edit)
		deletePendingReceipt(chatID)

	default:
		log.Printf("Unknown receipt callback data: %s", cq.Data)
	}
}

func formatReceiptDescription(pr *PendingReceipt) string {
	itemCount := len(pr.Items)
	if pr.Merchant != "" && itemCount > 0 {
		return fmt.Sprintf("%s (%d items)", pr.Merchant, itemCount)
	}
	if pr.Merchant != "" {
		return pr.Merchant
	}
	if itemCount > 0 {
		return fmt.Sprintf("Scan struk (%d items)", itemCount)
	}
	return "Scan struk"
}

func GetMainMenuKeyboard() tgbotapi.ReplyKeyboardMarkup {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Rekap Bulan Ini"),
			tgbotapi.NewKeyboardButton("🤖 AI Analisis"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💵 Cek Dompet"),
			tgbotapi.NewKeyboardButton("💰 Cek Net Worth"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔮 Proyeksi Cashflow"),
			tgbotapi.NewKeyboardButton("📋 Menu Budget"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🤝 Kelola Hutang"),
			tgbotapi.NewKeyboardButton("❌ Hapus Terakhir"),
		),
	)
	keyboard.ResizeKeyboard = true
	return keyboard
}

func (h *BotHandler) sendReply(chatID int64, text string, replyToMessageID int) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyToMessageID = replyToMessageID
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = GetMainMenuKeyboard()
	if _, err := h.bot.Send(msg); err != nil {
		log.Printf("Error sending message: %v", err)
	}
}

func (h *BotHandler) sendReplyWithKeyboard(chatID int64, text string, replyToMessageID int, keyboard tgbotapi.InlineKeyboardMarkup) {
	reply := tgbotapi.NewMessage(chatID, text)
	reply.ReplyToMessageID = replyToMessageID
	reply.ParseMode = "markdown"
	reply.ReplyMarkup = keyboard

	if _, err := h.bot.Send(reply); err != nil {
		log.Printf("Failed to send message with keyboard: %v", err)
	}
}

func receiptConfirmKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Simpan ✅", "ocr:confirm"),
			tgbotapi.NewInlineKeyboardButtonData("Edit Jumlah ✏️", "ocr:edit"),
			tgbotapi.NewInlineKeyboardButtonData("Batal ❌", "ocr:cancel"),
		),
	)
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

func parseAmount(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("jumlah tidak boleh kosong")
	}

	// Remove Rp prefix, dots, commas
	s = strings.TrimPrefix(s, "rp")
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", "")

	// Regex for units
	re := regexp.MustCompile(`^([\d.]+)\s*(rb|ribu|jt|juta|k|m)?$`)
	matches := re.FindStringSubmatch(s)
	if len(matches) == 0 {
		// Try parsing direct float
		val, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("format jumlah '%s' tidak valid", s)
		}
		return int64(val), nil
	}

	numStr := matches[1]
	unit := matches[2]

	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("format angka '%s' tidak valid", numStr)
	}

	var multiplier float64 = 1
	switch unit {
	case "k", "rb", "ribu":
		multiplier = 1000
	case "jt", "juta":
		multiplier = 1000000
	case "m":
		multiplier = 1000000000
	}

	return int64(val * multiplier), nil
}

func (h *BotHandler) handleBudgetCommand(telegramID int64, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "📋 *Panduan Perintah /budget*:\n\n" +
			"• `/budget set <kategori> <jumlah>` - Set budget untuk kategori tertentu\n" +
			"  _Contoh: `/budget set food 500rb`_\n\n" +
			"• `/budget cycle <tanggal>` - Set tanggal gajian / mulai siklus budget bulanan (1-31)\n" +
			"  _Contoh: `/budget cycle 25`_\n\n" +
			"• `/budget status` - Cek status budget & pengeluaran lu saat ini"
	}

	parts := strings.Fields(args)
	subcmd := strings.ToLower(parts[0])

	switch subcmd {
	case "set":
		if len(parts) < 3 {
			return "⚠️ Format salah. Gunakan: `/budget set <kategori> <jumlah>`\n_Contoh: `/budget set food 500rb`_"
		}
		category := strings.ToLower(parts[1])
		amountStr := parts[2]
		amount, err := parseAmount(amountStr)
		if err != nil {
			return fmt.Sprintf("⚠️ Jumlah budget tidak valid: %v", err)
		}

		reply, err := h.finance.SetCategoryBudget(telegramID, category, amount)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal menyetel budget: %v", err)
		}
		return reply

	case "cycle":
		if len(parts) < 2 {
			return "⚠️ Format salah. Gunakan: `/budget cycle <tanggal>`\n_Contoh: `/budget cycle 25`_"
		}
		dayStr := parts[1]
		day, err := strconv.Atoi(dayStr)
		if err != nil || day < 1 || day > 31 {
			return "⚠️ Tanggal siklus budget tidak valid. Harus berupa angka antara 1 sampai 31."
		}

		reply, err := h.finance.SetBudgetCycleStartDay(telegramID, day)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal menyetel siklus budget: %v", err)
		}
		return reply

	case "status":
		reply, err := h.finance.GetBudgetStatus(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal memanggil status budget: %v", err)
		}
		return reply

	default:
		return "⚠️ Perintah tidak dikenal. Gunakan `/budget set` atau `/budget status`."
	}
}

func parseDeadline(s string) (time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return time.Time{}, fmt.Errorf("deadline tidak boleh kosong")
	}

	// 1. Check relative months, e.g. "12m", "6m"
	reMonths := regexp.MustCompile(`^(\d+)m$`)
	if matches := reMonths.FindStringSubmatch(s); len(matches) > 0 {
		monthsVal, err := strconv.Atoi(matches[1])
		if err != nil {
			return time.Time{}, fmt.Errorf("format bulan '%s' tidak valid", matches[1])
		}
		now := time.Now()
		future := now.AddDate(0, monthsVal, 0)
		return time.Date(future.Year(), future.Month(), future.Day(), 23, 59, 59, 0, time.UTC), nil
	}

	// 2. Check full YYYY-MM-DD
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC), nil
	}

	// 3. Check YYYY-MM
	if t, err := time.Parse("2006-01", s); err == nil {
		nextMonth := time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		lastDay := nextMonth.Add(-24 * time.Hour)
		return time.Date(lastDay.Year(), lastDay.Month(), lastDay.Day(), 23, 59, 59, 0, time.UTC), nil
	}

	return time.Time{}, fmt.Errorf("format deadline '%s' tidak didukung. Gunakan YYYY-MM-DD (contoh: 2026-12-31) atau format bulan relative (contoh: 12m)", s)
}

func (h *BotHandler) handleGoalCommand(telegramID int64, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "📋 *Panduan Perintah /goal*:\n\n" +
			"• `/goal add <nama> <jumlah> <deadline>` - Set target keuangan baru\n" +
			"  _Nama bisa mengandung spasi, deadline bisa YYYY-MM-DD atau jumlah bulan relative seperti 12m._\n" +
			"  _Contoh: `/goal add Beli Laptop 12jt 2026-12-31`_\n" +
			"  _Contoh: `/goal add DP Motor 5jt 6m`_\n\n" +
			"• `/goal status` - Cek progress & saran tabungan target keuangan lu saat ini"
	}

	parts := strings.Fields(args)
	subcmd := strings.ToLower(parts[0])

	switch subcmd {
	case "add":
		if len(parts) < 4 {
			return "⚠️ Format salah. Gunakan: `/goal add <nama> <jumlah> <deadline>`\n_Contoh: `/goal add Beli Laptop 12jt 2026-12-31`_"
		}
		
		deadlineStr := parts[len(parts)-1]
		amountStr := parts[len(parts)-2]
		nameParts := parts[1 : len(parts)-2]
		name := strings.Join(nameParts, " ")

		deadline, err := parseDeadline(deadlineStr)
		if err != nil {
			return fmt.Sprintf("⚠️ Deadline tidak valid: %v", err)
		}

		amount, err := parseAmount(amountStr)
		if err != nil {
			return fmt.Sprintf("⚠️ Jumlah target tidak valid: %v", err)
		}

		reply, err := h.finance.AddGoal(telegramID, name, amount, deadline)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal menambahkan target keuangan: %v", err)
		}
		return reply

	case "status":
		reply, err := h.finance.GetGoalStatus(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal memanggil status target keuangan: %v", err)
		}
		return reply

	default:
		return "⚠️ Perintah tidak dikenal. Gunakan `/goal add` atau `/goal status`."
	}
}

func (h *BotHandler) handleDeleteCommand(chatID int64, telegramID int64, args string, replyToMessageID int) {
	args = strings.TrimSpace(args)
	if args != "" {
		tx, err := h.finance.DeleteTransaction(telegramID, args)
		if err != nil {
			h.sendReply(chatID, fmt.Sprintf("⚠️ *Gagal menghapus transaksi:* %v\n_Pastikan UUID transaksi benar ya bro!_", err), replyToMessageID)
			return
		}

		typeEmoji := "💸 pengeluaran"
		if tx.Type == "income" {
			typeEmoji = "💰 pemasukan"
		}
		replyText := fmt.Sprintf("✅ *Transaksi Berhasil Dihapus!* 🎉\n\n"+
			"• *Tipe*: %s\n"+
			"• *Kategori*: %s\n"+
			"• *Jumlah*: %s\n"+
			"• *Deskripsi*: %s\n\n"+
			"💾 _Saldo dompet dan budget sudah diupdate secara otomatis._",
			typeEmoji, tx.Category, formatIDRCurrency(tx.Amount), tx.Description)
		h.sendReply(chatID, replyText, replyToMessageID)
		return
	}

	// Fetch last 5 transactions
	txs, err := h.finance.GetTransactions(telegramID, 5, "")
	if err != nil {
		h.sendReply(chatID, fmt.Sprintf("⚠️ *Gagal mengambil daftar transaksi:* %v", err), replyToMessageID)
		return
	}
	if len(txs) == 0 {
		h.sendReply(chatID, "📂 *Belum ada catatan transaksi sama sekali nih bro.*", replyToMessageID)
		return
	}

	var sb strings.Builder
	sb.WriteString("🗑️ *Pilih Transaksi yang Ingin Dihapus:*\n\n")

	var rows [][]tgbotapi.InlineKeyboardButton
	for i, tx := range txs {
		typeSign := "💸"
		if tx.Type == "income" {
			typeSign = "💰"
		}
		sb.WriteString(fmt.Sprintf("%d. %s *%s*: %s - %s (_%s_)\n",
			i+1, typeSign, tx.Category, formatIDRCurrency(tx.Amount), tx.Description, tx.TransactionDate.Format("02 Jan 15:04")))

		buttonText := fmt.Sprintf("Hapus %d ❌", i+1)
		callbackData := fmt.Sprintf("tx:delete:%s", tx.ID)
		row := tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(buttonText, callbackData))
		rows = append(rows, row)
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	h.sendReplyWithKeyboard(chatID, sb.String(), replyToMessageID, keyboard)
}

func (h *BotHandler) handleAssetCommand(telegramID int64, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "📋 *Panduan Perintah /asset*:\n\n" +
			"• `/asset add <tipe> <nama> <jumlah>`\n  _Tambah aset baru (contoh tipe: bank, cash, investment, property, etc.)_\n  _Contoh: `/asset add bank BCA 12000000`_\n\n" +
			"• `/asset set <nama> <jumlah>`\n  _Update nilai jumlah aset yang sudah ada_\n  _Contoh: `/asset set BCA 15000000`_\n\n" +
			"• `/asset list`\n  _Lihat daftar semua aset lu_\n\n" +
			"• `/asset delete <nama>`\n  _Hapus aset dari daftar_\n  _Contoh: `/asset delete BCA`_"
	}

	parts := strings.Fields(args)
	subcmd := strings.ToLower(parts[0])

	switch subcmd {
	case "add":
		if len(parts) < 4 {
			return "⚠️ Format salah.\nGunakan: `/asset add <tipe> <nama> <jumlah>`\n_Contoh: `/asset add bank BCA 12000000`_"
		}
		assetType := parts[1]
		name := parts[2]
		amount, err := parseAmount(parts[3])
		if err != nil {
			return fmt.Sprintf("⚠️ Jumlah tidak valid: %v\n_Coba: 12jt, 12000000, 1.5jt_", err)
		}

		asset, err := h.finance.AddAsset(telegramID, assetType, name, amount, "")
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal menambahkan aset: %v", err)
		}
		return fmt.Sprintf("✅ *Aset Berhasil Ditambahkan!* 🎉\n\n• *Nama:* %s\n• *Tipe:* %s\n• *Jumlah:* %s", asset.Name, asset.AssetType, formatIDRCurrency(asset.Amount))

	case "set":
		if len(parts) < 3 {
			return "⚠️ Format salah.\nGunakan: `/asset set <nama> <jumlah>`\n_Contoh: `/asset set BCA 15000000`_"
		}
		name := parts[1]
		amount, err := parseAmount(parts[2])
		if err != nil {
			return fmt.Sprintf("⚠️ Jumlah tidak valid: %v\n_Coba: 15jt, 15000000_", err)
		}

		assets, err := h.finance.GetAssets(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal mengambil daftar aset: %v", err)
		}

		var targetAsset *models.Asset
		for _, a := range assets {
			if strings.EqualFold(a.Name, name) {
				targetAsset = a
				break
			}
		}

		if targetAsset == nil {
			return fmt.Sprintf("⚠️ Aset dengan nama *%s* tidak ditemukan.", name)
		}

		err = h.finance.UpdateAssetAmount(telegramID, targetAsset.ID, amount)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal mengupdate aset: %v", err)
		}

		return fmt.Sprintf("✅ *Aset Berhasil Diupdate!* 📈\n\n• *Nama:* %s\n• *Jumlah Baru:* %s", targetAsset.Name, formatIDRCurrency(amount))

	case "list":
		assets, err := h.finance.GetAssets(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal mengambil daftar aset: %v", err)
		}

		if len(assets) == 0 {
			return "📂 *Daftar aset lu masih kosong, bro.*"
		}

		var msg strings.Builder
		msg.WriteString("📂 *Daftar Aset Lu:*\n\n")
		var total int64
		for _, a := range assets {
			msg.WriteString(fmt.Sprintf("• [%s] *%s*: %s\n", a.AssetType, a.Name, formatIDRCurrency(a.Amount)))
			total += a.Amount
		}
		msg.WriteString(fmt.Sprintf("\n🧮 *Total Aset:* %s", formatIDRCurrency(total)))
		return msg.String()

	case "delete":
		if len(parts) < 2 {
			return "⚠️ Format salah.\nGunakan: `/asset delete <nama>`\n_Contoh: `/asset delete BCA`_"
		}
		name := parts[1]

		assets, err := h.finance.GetAssets(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal mengambil daftar aset: %v", err)
		}

		var targetAsset *models.Asset
		for _, a := range assets {
			if strings.EqualFold(a.Name, name) {
				targetAsset = a
				break
			}
		}

		if targetAsset == nil {
			return fmt.Sprintf("⚠️ Aset dengan nama *%s* tidak ditemukan.", name)
		}

		err = h.finance.DeleteAsset(telegramID, targetAsset.ID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal menghapus aset: %v", err)
		}

		return fmt.Sprintf("✅ *Aset Berhasil Dihapus!* ❌\n\n• *Nama:* %s", targetAsset.Name)

	default:
		return "⚠️ Sub-perintah tidak dikenal.\n\nGunakan:\n• `/asset add <tipe> <nama> <jumlah>`\n• `/asset set <nama> <jumlah>`\n• `/asset list`\n• `/asset delete <nama>`"
	}
}

func (h *BotHandler) handleLiabilityCommand(telegramID int64, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "📋 *Panduan Perintah /liability*:\n\n" +
			"• `/liability add <tipe> <nama> <jumlah>`\n  _Tambah kewajiban/hutang baru (contoh tipe: loan, credit_card, debt, etc.)_\n  _Contoh: `/liability add loan Cicilan_Motor 2000000`_\n\n" +
			"• `/liability set <nama> <jumlah>`\n  _Update nilai jumlah kewajiban yang sudah ada_\n  _Contoh: `/liability set Cicilan_Motor 1500000`_\n\n" +
			"• `/liability list`\n  _Lihat daftar semua kewajiban lu_\n\n" +
			"• `/liability delete <nama>`\n  _Hapus kewajiban dari daftar_\n  _Contoh: `/liability delete Cicilan_Motor`_"
	}

	parts := strings.Fields(args)
	subcmd := strings.ToLower(parts[0])

	switch subcmd {
	case "add":
		if len(parts) < 4 {
			return "⚠️ Format salah.\nGunakan: `/liability add <tipe> <nama> <jumlah>`\n_Contoh: `/liability add loan Cicilan_Motor 2000000`_"
		}
		liabilityType := parts[1]
		name := parts[2]
		amount, err := parseAmount(parts[3])
		if err != nil {
			return fmt.Sprintf("⚠️ Jumlah tidak valid: %v\n_Coba: 2jt, 2000000, 1.5jt_", err)
		}

		liability, err := h.finance.AddLiability(telegramID, liabilityType, name, amount, nil, "")
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal menambahkan kewajiban: %v", err)
		}
		return fmt.Sprintf("✅ *Kewajiban Berhasil Ditambahkan!* 🎉\n\n• *Nama:* %s\n• *Tipe:* %s\n• *Jumlah:* %s", liability.Name, liability.LiabilityType, formatIDRCurrency(liability.Amount))

	case "set":
		if len(parts) < 3 {
			return "⚠️ Format salah.\nGunakan: `/liability set <nama> <jumlah>`\n_Contoh: `/liability set Cicilan_Motor 1500000`_"
		}
		name := parts[1]
		amount, err := parseAmount(parts[2])
		if err != nil {
			return fmt.Sprintf("⚠️ Jumlah tidak valid: %v\n_Coba: 1.5jt, 1500000_", err)
		}

		liabilities, err := h.finance.GetLiabilities(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal mengambil daftar kewajiban: %v", err)
		}

		var targetLiab *models.Liability
		for _, l := range liabilities {
			if strings.EqualFold(l.Name, name) {
				targetLiab = l
				break
			}
		}

		if targetLiab == nil {
			return fmt.Sprintf("⚠️ Kewajiban dengan nama *%s* tidak ditemukan.", name)
		}

		err = h.finance.UpdateLiabilityAmount(telegramID, targetLiab.ID, amount)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal mengupdate kewajiban: %v", err)
		}

		return fmt.Sprintf("✅ *Kewajiban Berhasil Diupdate!* 📈\n\n• *Nama:* %s\n• *Jumlah Baru:* %s", targetLiab.Name, formatIDRCurrency(amount))

	case "list":
		liabilities, err := h.finance.GetLiabilities(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal mengambil daftar kewajiban: %v", err)
		}

		if len(liabilities) == 0 {
			return "📂 *Daftar kewajiban lu masih kosong, bro.*"
		}

		var msg strings.Builder
		msg.WriteString("💸 *Daftar Kewajiban Lu:*\n\n")
		var total int64
		for _, l := range liabilities {
			dueStr := ""
			if l.DueDate != nil {
				dueStr = fmt.Sprintf(" - Jt Tempo: %s", l.DueDate.Format("02 Jan 2006"))
			}
			msg.WriteString(fmt.Sprintf("• [%s] *%s*: %s%s\n", l.LiabilityType, l.Name, formatIDRCurrency(l.Amount), dueStr))
			total += l.Amount
		}
		msg.WriteString(fmt.Sprintf("\n🧮 *Total Kewajiban:* %s", formatIDRCurrency(total)))
		return msg.String()

	case "delete":
		if len(parts) < 2 {
			return "⚠️ Format salah.\nGunakan: `/liability delete <nama>`\n_Contoh: `/liability delete Cicilan_Motor`_"
		}
		name := parts[1]

		liabilities, err := h.finance.GetLiabilities(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal mengambil daftar kewajiban: %v", err)
		}

		var targetLiab *models.Liability
		for _, l := range liabilities {
			if strings.EqualFold(l.Name, name) {
				targetLiab = l
				break
			}
		}

		if targetLiab == nil {
			return fmt.Sprintf("⚠️ Kewajiban dengan nama *%s* tidak ditemukan.", name)
		}

		err = h.finance.DeleteLiability(telegramID, targetLiab.ID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal menghapus kewajiban: %v", err)
		}

		return fmt.Sprintf("✅ *Kewajiban Berhasil Dihapus!* ❌\n\n• *Nama:* %s", targetLiab.Name)

	default:
		return "⚠️ Sub-perintah tidak dikenal.\n\nGunakan:\n• `/liability add <tipe> <nama> <jumlah>`\n• `/liability set <nama> <jumlah>`\n• `/liability list`\n• `/liability delete <nama>`"
	}
}

func (h *BotHandler) handleNetWorthCommand(telegramID int64, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		reply, err := h.finance.GetNetWorthStatus(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal mengambil status net worth: %v", err)
		}
		return reply
	}

	parts := strings.Fields(args)
	subcmd := strings.ToLower(parts[0])

	switch subcmd {
	case "history":
		reply, err := h.finance.GetNetWorthHistory(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal mengambil riwayat net worth: %v", err)
		}
		return reply

	default:
		return "⚠️ Sub-perintah tidak dikenal.\n\nGunakan:\n• `/networth` — Lihat status net worth saat ini\n• `/networth history` — Lihat riwayat perkembangan net worth"
	}
}

func (h *BotHandler) handleCashflowCommand(telegramID int64, args string) string {
	args = strings.TrimSpace(args)
	tz := os.Getenv("TZ")
	if tz == "" {
		tz = "Asia/Jakarta"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)

	var targetDate time.Time

	if args == "" {
		// Zero time, will be resolved in PredictCashflow using user's payday_day or end of month
		targetDate = time.Time{}
	} else {
		parts := strings.Fields(args)
		if len(parts) == 1 {
			// cashflow <days>
			days, err := strconv.Atoi(parts[0])
			if err != nil || days <= 0 {
				return "⚠️ *Format perintah salah!*\n\nGunakan:\n• `/cashflow` — Prediksi sampai akhir bulan ini\n• `/cashflow <hari>` — Prediksi n hari ke depan (misal: `/cashflow 30`)\n• `/cashflow payday <tanggal>` — Prediksi sampai gajian berikutnya (misal: `/cashflow payday 25`)"
			}
			targetDate = now.AddDate(0, 0, days)
		} else if len(parts) == 2 && strings.ToLower(parts[0]) == "payday" {
			// cashflow payday <date>
			payday, err := strconv.Atoi(parts[1])
			if err != nil || payday < 1 || payday > 31 {
				return "⚠️ *Tanggal gajian tidak valid!* Pilih tanggal antara 1 s/d 31."
			}
			// If today's day is less than payday, it's this month. Else next month.
			if now.Day() < payday {
				targetDate = time.Date(now.Year(), now.Month(), payday, 23, 59, 59, 0, loc)
			} else {
				targetDate = time.Date(now.Year(), now.Month(), payday, 23, 59, 59, 0, loc).AddDate(0, 1, 0)
			}
		} else {
			return "⚠️ *Format perintah salah!*\n\nGunakan:\n• `/cashflow` — Prediksi sampai akhir bulan ini\n• `/cashflow <hari>` — Prediksi n hari ke depan (misal: `/cashflow 30`)\n• `/cashflow payday <tanggal>` — Prediksi sampai gajian berikutnya (misal: `/cashflow payday 25`)"
		}
	}

	_, msg, err := h.finance.PredictCashflow(telegramID, targetDate)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal menghitung proyeksi cashflow:*\n%v", err)
	}
	return msg
}

func (h *BotHandler) handlePaydayCommand(telegramID int64, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "📅 *Panduan Perintah /payday*:\n\n" +
			"• `/payday set <tanggal>` - Set tanggal gajian bulanan kamu (1-31)\n" +
			"  _Contoh: `/payday set 25`_\n\n" +
			"• `/payday show` - Tampilkan tanggal gajian kamu saat ini"
	}

	parts := strings.Fields(args)
	subcmd := strings.ToLower(parts[0])

	switch subcmd {
	case "set":
		if len(parts) < 2 {
			return "⚠️ Format salah. Gunakan: `/payday set <tanggal>`\n_Contoh: `/payday set 25`_"
		}
		dayStr := parts[1]
		day, err := strconv.Atoi(dayStr)
		if err != nil || day < 1 || day > 31 {
			return "⚠️ Tanggal gajian tidak valid. Harus berupa angka antara 1 sampai 31."
		}

		reply, err := h.finance.SetPaydayDay(telegramID, day)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal menyetel tanggal gajian: %v", err)
		}
		return reply

	case "show":
		reply, err := h.finance.GetPaydayDay(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ Gagal mengambil tanggal gajian: %v", err)
		}
		return reply

	default:
		return "⚠️ Perintah tidak dikenal. Gunakan `/payday set <tanggal>` atau `/payday show`."
	}
}

