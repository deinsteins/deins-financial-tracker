package handlers

import (
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"finance-bot/bot/services"
)

type BotHandler struct {
	bot     *tgbotapi.BotAPI
	finance services.FinanceService
}

func NewBotHandler(bot *tgbotapi.BotAPI, finance services.FinanceService) *BotHandler {
	return &BotHandler{
		bot:     bot,
		finance: finance,
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
	log.Printf("Parsing text message: %s", msg.Text)
	replyText, err := h.finance.AnalyzeText(msg.From.ID, msg.Text)
	if err != nil {
		replyText = fmt.Sprintf("⚠️ *Waduh, ada kendala pas nyatet transaksi lu nih:*\n%v", err)
	}
	h.sendReply(msg.Chat.ID, replyText, msg.MessageID)
}

func (h *BotHandler) sendReply(chatID int64, text string, replyToMessageID int) {
	reply := tgbotapi.NewMessage(chatID, text)
	reply.ReplyToMessageID = replyToMessageID
	reply.ParseMode = "markdown"

	if _, err := h.bot.Send(reply); err != nil {
		log.Printf("Failed to send message: %v", err)
	}
}
