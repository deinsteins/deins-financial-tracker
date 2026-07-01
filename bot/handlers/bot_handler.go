package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
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

	case "pay":
		return h.handleDebtPay(telegramID, parts[1:])

	case "paid":
		return h.handleDebtPaid(telegramID, parts[1:])

	case "cancel":
		return h.handleDebtCancel(telegramID, parts[1:])

	default:
		return "⚠️ Sub-perintah tidak dikenal.\n\n" +
			"Coba `/debt add receivable Andi 500rb makan`, `/debt list`, atau `/debt summary` ya bro!"
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

func (h *BotHandler) sendReply(chatID int64, text string, replyToMessageID int) {
	reply := tgbotapi.NewMessage(chatID, text)
	reply.ReplyToMessageID = replyToMessageID
	reply.ParseMode = "markdown"

	if _, err := h.bot.Send(reply); err != nil {
		log.Printf("Failed to send message: %v", err)
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
			"  _Contoh: `/budget set food 500rb`_\n" +
			"  _Contoh: `/budget set transport 200k`_\n\n" +
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
