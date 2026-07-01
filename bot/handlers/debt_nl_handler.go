package handlers

import (
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"finance-bot/bot/services"
)

// debtKeywords are the trigger words that cause a free-text message to be
// routed through the debt/receivable intent parser before falling back to
// normal expense/income parsing.
var debtKeywords = []string{"hutang", "utang", "pinjam", "bayar", "lunas", "nagih"}

func containsDebtKeyword(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range debtKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func parseDueDate(s *string) *time.Time {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", strings.TrimSpace(*s))
	if err != nil {
		return nil
	}
	return &t
}

// tryHandleDebtIntent attempts to parse and execute a debt/receivable intent
// from free-form text. It returns true if the message was fully handled (a
// reply was already sent to the user), or false if the caller should fall
// back to normal transaction/orchestration parsing — e.g. the text doesn't
// contain a debt keyword, the AI service call failed, or the parsed intent
// came back "unknown" with no identifiable person (which usually means the
// keyword match was a false positive, like "bayar listrik 100rb").
func (h *BotHandler) tryHandleDebtIntent(msg *tgbotapi.Message, text string) bool {
	if !containsDebtKeyword(text) {
		return false
	}

	parsed, err := h.finance.ParseDebtText(text)
	if err != nil {
		log.Printf("WARNING: debt intent parsing failed, falling back to normal parsing: %v", err)
		return false
	}

	replyText, handled := h.buildDebtReply(msg.From.ID, parsed)
	if !handled {
		return false
	}

	h.sendReply(msg.Chat.ID, replyText, msg.MessageID)

	_ = h.finance.SaveChatHistory(msg.From.ID, "user", msg.Text)
	_ = h.finance.SaveChatHistory(msg.From.ID, "assistant", replyText)
	return true
}

func (h *BotHandler) buildDebtReply(telegramID int64, parsed *services.DebtParseResponse) (string, bool) {
	switch parsed.Intent {
	case "add_debt":
		return h.debtIntentAdd(telegramID, parsed)
	case "pay_debt":
		return h.debtIntentPay(telegramID, parsed)
	case "mark_paid":
		return h.debtIntentMarkPaid(telegramID, parsed)
	case "cancel_debt":
		return h.debtIntentCancel(telegramID, parsed)
	case "list_debt":
		return h.handleDebtList(telegramID), true
	case "debt_summary":
		return h.handleDebtSummary(telegramID), true
	default: // "unknown"
		if stringOrEmpty(parsed.PersonName) == "" {
			// No person identified at all -> the keyword match was likely a
			// false positive (e.g. "bayar listrik 100rb"); let normal
			// expense/income parsing handle it instead.
			return "", false
		}
		reason := "Gua kurang yakin maksud lu apa nih bro."
		if r := stringOrEmpty(parsed.Reason); r != "" {
			reason = r
		}
		return fmt.Sprintf("🤔 *Kurang Paham Nih*\n\n%s\n\n"+
			"_Coba lebih spesifik ya, misal: \"Andi hutang ke saya 100rb\" atau pakai perintah `/debt` langsung._",
			reason), true
	}
}

func (h *BotHandler) debtIntentAdd(telegramID int64, parsed *services.DebtParseResponse) (string, bool) {
	personName := stringOrEmpty(parsed.PersonName)
	direction := stringOrEmpty(parsed.Direction)
	if personName == "" || direction == "" || parsed.Amount == nil || *parsed.Amount <= 0 {
		return "⚠️ Gua nangkep ini kayak soal hutang, tapi info nama/jumlahnya kurang jelas nih bro.\n\n" +
			"_Coba lebih detail, misal: \"Andi hutang ke saya 200rb buat makan\"._", true
	}

	description := stringOrEmpty(parsed.Description)
	dueDate := parseDueDate(parsed.DueDate)

	debt, err := h.finance.AddDebt(telegramID, personName, direction, *parsed.Amount, description, dueDate)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal menyimpan hutang:*\n%v", err), true
	}

	dirEmoji := "💸 Hutang"
	dirLabel := fmt.Sprintf("Hutang ke *%s*", debt.PersonName)
	if direction == "receivable" {
		dirEmoji = "💰 Piutang"
		dirLabel = fmt.Sprintf("*%s* hutang ke lu", debt.PersonName)
	}

	reply := fmt.Sprintf("✅ *Hutang Berhasil Dicatat!* 🎉\n\n"+
		"• *Tipe*: %s\n"+
		"• %s\n"+
		"• *Jumlah*: %s\n",
		dirEmoji, dirLabel, formatIDRCurrency(debt.Amount))

	if description != "" {
		reply += fmt.Sprintf("• *Deskripsi*: %s\n", description)
	}
	if debt.DueDate != nil {
		reply += fmt.Sprintf("• *Jatuh Tempo*: %s\n", debt.DueDate.Format("02 Jan 2006"))
	}
	reply += "\n_Dicatat otomatis dari pesan lu ya bro!_ 👍"

	return reply, true
}

func (h *BotHandler) debtIntentPay(telegramID int64, parsed *services.DebtParseResponse) (string, bool) {
	personName := stringOrEmpty(parsed.PersonName)
	if personName == "" || parsed.Amount == nil || *parsed.Amount <= 0 {
		return "⚠️ Gua nangkep ini kayak soal bayar hutang, tapi nama/jumlahnya kurang jelas nih bro.\n\n" +
			"_Coba lebih detail, misal: \"Andi bayar 100rb\"._", true
	}

	debts, err := h.finance.GetDebtsByPersonName(telegramID, personName)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mencari hutang:*\n%v", err), true
	}
	if len(debts) == 0 {
		return fmt.Sprintf("⚠️ Tidak ditemukan hutang aktif atas nama *%s*.", personName), true
	}
	if len(debts) > 1 {
		return fmt.Sprintf("⚠️ Ada %d hutang aktif atas nama *%s*.\nCoba pakai perintah `/debt pay %s <jumlah>` biar lebih spesifik ya bro!", len(debts), personName, personName), true
	}

	debt := debts[0]
	note := fmt.Sprintf("Bayar %s", formatIDRCurrency(*parsed.Amount))

	payment, updatedDebt, err := h.finance.PayDebt(telegramID, debt.ID, *parsed.Amount, note)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal memproses pembayaran:*\n%v", err), true
	}

	remaining := updatedDebt.Amount - updatedDebt.PaidAmount
	statusLabel := "🟡 Belum lunas"
	if updatedDebt.Status == "paid" {
		statusLabel = "🟢 Lunas!"
	}

	reply := fmt.Sprintf("✅ *Pembayaran Berhasil!* 🎉\n\n"+
		"• *Nama:* %s\n"+
		"• *Dibayar:* %s\n"+
		"• *Sisa:* %s\n"+
		"• *Status:* %s",
		debt.PersonName, formatIDRCurrency(payment.Amount), formatIDRCurrency(remaining), statusLabel)

	if updatedDebt.Status == "paid" {
		reply += "\n\n🎉 *Hutang ini sudah lunas!*"
	}

	return reply, true
}

func (h *BotHandler) debtIntentMarkPaid(telegramID int64, parsed *services.DebtParseResponse) (string, bool) {
	personName := stringOrEmpty(parsed.PersonName)
	if personName == "" {
		return "⚠️ Gua nangkep ini kayak soal hutang lunas, tapi nama orangnya kurang jelas nih bro.\n\n" +
			"_Coba lebih detail, misal: \"utang Andi sudah lunas\"._", true
	}

	debts, err := h.finance.GetDebtsByPersonName(telegramID, personName)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mencari hutang:*\n%v", err), true
	}
	if len(debts) == 0 {
		return fmt.Sprintf("⚠️ Tidak ditemukan hutang aktif atas nama *%s*.", personName), true
	}
	if len(debts) > 1 {
		return fmt.Sprintf("⚠️ Ada %d hutang aktif atas nama *%s*.\nCoba pakai perintah `/debt paid %s` biar lebih spesifik ya bro!", len(debts), personName, personName), true
	}

	debt := debts[0]
	if err := h.finance.SettleDebt(telegramID, debt.ID); err != nil {
		return fmt.Sprintf("⚠️ *Gagal menandai lunas:*\n%v", err), true
	}

	return fmt.Sprintf("✅ *Hutang Lunas!* 🎉\n\n"+
		"• *Nama:* %s\n"+
		"• *Jumlah:* %s\n\n"+
		"Hutang ini udah gua tandain *lunas* ya bro! 👍",
		debt.PersonName, formatIDRCurrency(debt.Amount)), true
}

func (h *BotHandler) debtIntentCancel(telegramID int64, parsed *services.DebtParseResponse) (string, bool) {
	personName := stringOrEmpty(parsed.PersonName)
	if personName == "" {
		return "⚠️ Gua nangkep ini kayak soal batalin hutang, tapi nama orangnya kurang jelas nih bro.\n\n" +
			"_Coba lebih detail, misal: \"hapus hutang Budi\"._", true
	}

	debts, err := h.finance.GetDebtsByPersonName(telegramID, personName)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mencari hutang:*\n%v", err), true
	}
	if len(debts) == 0 {
		return fmt.Sprintf("⚠️ Tidak ditemukan hutang aktif atas nama *%s*.", personName), true
	}
	if len(debts) > 1 {
		return fmt.Sprintf("⚠️ Ada %d hutang aktif atas nama *%s*.\nCoba pakai perintah `/debt cancel %s` biar lebih spesifik ya bro!", len(debts), personName, personName), true
	}

	debt := debts[0]
	if err := h.finance.CancelDebt(telegramID, debt.ID); err != nil {
		return fmt.Sprintf("⚠️ *Gagal membatalkan hutang:*\n%v", err), true
	}

	return fmt.Sprintf("❌ *Hutang Dibatalkan*\n\n"+
		"• *Nama:* %s\n"+
		"• *Jumlah:* %s\n\n"+
		"Hutang ini udah gua batalin ya bro!",
		debt.PersonName, formatIDRCurrency(debt.Amount)), true
}
