package handlers

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
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

func (h *BotHandler) HandleUpdates(updates tgbotapi.UpdatesChannel) {
	for update := range updates {
		if update.Message == nil { // Ignore non-message updates
			continue
		}

		// Log incoming message
		log.Printf("[%s] %s (ChatID: %d)", update.Message.From.UserName, update.Message.Text, update.Message.Chat.ID)

		// Handle command or message
		if update.Message.IsCommand() {
			h.handleCommand(update.Message)
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

	case "analyze":
		replyText, err = h.finance.GenerateAIAnalysis(msg.From.ID)
		if err != nil {
			replyText = fmt.Sprintf("⚠️ *Error pas analisis keuangan lu nih:*\n%v", err)
		}

	default:
		replyText = "Perintah apaan tuh? Coba cek /start aja biar jelas bro!"
	}

	h.sendReply(msg.Chat.ID, replyText, msg.MessageID)
}

func (h *BotHandler) handleTextMessage(msg *tgbotapi.Message) {
	log.Printf("Parsing text message with Hermes: %s", msg.Text)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Fetch conversational history memory context
	history, err := h.finance.GetChatHistory(msg.From.ID)
	if err != nil {
		log.Printf("WARNING: failed to load chat memory context: %v", err)
		history = nil // fallback to empty context
	}

	intent, err := h.orchestration.ParseIntent(ctx, history, msg.Text)
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
				replyText += fmt.Sprintf("✅ *Catatan Berhasil Disimpan!* 🎉\n\n"+
					"• *Tipe*: %s\n"+
					"• *Kategori*: %s\n"+
					"• *Jumlah*: %s\n"+
					"• *Deskripsi*: %s\n\n",
					typeEmoji, tx.Category, formatIDRCurrency(tx.Amount), tx.Description)

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

func (h *BotHandler) sendReply(chatID int64, text string, replyToMessageID int) {
	reply := tgbotapi.NewMessage(chatID, text)
	reply.ReplyToMessageID = replyToMessageID
	reply.ParseMode = "markdown"

	if _, err := h.bot.Send(reply); err != nil {
		log.Printf("Failed to send message: %v", err)
	}
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
