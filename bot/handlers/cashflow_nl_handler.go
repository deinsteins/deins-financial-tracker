package handlers

import (
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"finance-bot/bot/services"
)

var cashflowKeywords = []string{
	"cashflow", "cash flow", "proyeksi", "prediksi",
	"cukup", "gajian", "gaji", "payday", "akhir bulan",
}

func containsCashflowKeyword(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range cashflowKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func (h *BotHandler) tryHandleCashflowIntent(msg *tgbotapi.Message, text string) bool {
	if !containsCashflowKeyword(text) {
		return false
	}

	parsed, err := h.finance.ParseCashflowText(text)
	if err != nil {
		log.Printf("WARNING: cashflow intent parsing failed, falling back to normal parsing: %v", err)
		return false
	}

	replyText, handled := h.buildCashflowNLReply(msg.From.ID, parsed)
	if !handled {
		return false
	}

	h.sendReply(msg.Chat.ID, replyText, msg.MessageID)

	_ = h.finance.SaveChatHistory(msg.From.ID, "user", msg.Text)
	_ = h.finance.SaveChatHistory(msg.From.ID, "assistant", replyText)
	return true
}

func (h *BotHandler) buildCashflowNLReply(telegramID int64, parsed *services.CashflowParseResponse) (string, bool) {
	switch parsed.Intent {
	case "show_cashflow":
		var targetDate time.Time
		if parsed.ResolvedTargetDate != nil && *parsed.ResolvedTargetDate != "" {
			t, err := time.Parse("2006-01-02", *parsed.ResolvedTargetDate)
			if err == nil {
				targetDate = t
			}
		}

		// Predict cashflow
		_, replyText, err := h.finance.PredictCashflow(telegramID, targetDate)
		if err != nil {
			return fmt.Sprintf("⚠️ *Gagal menghitung proyeksi cashflow:*\n%v", err), true
		}
		return replyText, true

	case "set_payday":
		if parsed.PaydayDay == nil {
			return "⚠️ Info tanggal gajian kurang jelas nih bro.\n\n" +
				"_Coba lebih detail, misal: \"gajian saya tanggal 25\" atau \"set gajian tanggal 25\"._", true
		}
		replyText, err := h.finance.SetPaydayDay(telegramID, *parsed.PaydayDay)
		if err != nil {
			return fmt.Sprintf("⚠️ *Gagal menyetel tanggal gajian:*\n%v", err), true
		}
		return replyText, true

	default: // "unknown"
		return "", false
	}
}
