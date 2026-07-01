package scheduler

import (
	"fmt"
	"log"
	"math"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"finance-bot/bot/models"
	"finance-bot/bot/repositories"
)

// DebtReminderScheduler runs a daily job (at 08:00 server time) that finds
// active debts due today, due tomorrow, or overdue, and sends each affected
// user a Telegram reminder — at most once per debt per day.
type DebtReminderScheduler struct {
	bot          *tgbotapi.BotAPI
	debtRepo     repositories.DebtRepository
	reminderRepo repositories.DebtReminderRepository
}

func NewDebtReminderScheduler(bot *tgbotapi.BotAPI, debtRepo repositories.DebtRepository, reminderRepo repositories.DebtReminderRepository) *DebtReminderScheduler {
	return &DebtReminderScheduler{
		bot:          bot,
		debtRepo:     debtRepo,
		reminderRepo: reminderRepo,
	}
}

// Start launches the daily reminder loop in a background goroutine. Call once at startup.
func (s *DebtReminderScheduler) Start() {
	go s.run()
}

func (s *DebtReminderScheduler) run() {
	for {
		next := nextRunTime(time.Now())
		wait := time.Until(next)
		log.Printf("[DebtReminderScheduler] next run at %s (in %s)", next.Format(time.RFC3339), wait.Round(time.Second))
		time.Sleep(wait)
		s.RunOnce()
	}
}

// nextRunTime returns the next 08:00 (server-local time) at or after now.
func nextRunTime(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// RunOnce checks all active debts due today, tomorrow, or overdue, and sends
// a reminder for each one that hasn't already been reminded today.
func (s *DebtReminderScheduler) RunOnce() {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tomorrow := today.AddDate(0, 0, 1)

	due, err := s.debtRepo.GetDueDebtsForReminders(tomorrow)
	if err != nil {
		log.Printf("[DebtReminderScheduler] ERROR: failed to fetch due debts: %v", err)
		return
	}

	log.Printf("[DebtReminderScheduler] checking %d candidate debt(s) for reminders", len(due))

	for _, item := range due {
		s.processDebt(item, today)
	}
}

func (s *DebtReminderScheduler) processDebt(item *repositories.DueDebt, today time.Time) {
	debt := item.Debt
	if debt.DueDate == nil {
		return
	}

	y, m, d := debt.DueDate.Date()
	dueDateOnly := time.Date(y, m, d, 0, 0, 0, 0, today.Location())
	diffDays := int(math.Round(dueDateOnly.Sub(today).Hours() / 24))

	var reminderType, text string
	switch {
	case diffDays == 0:
		reminderType = "due_today"
		text = formatDueTodayReminder(debt)
	case diffDays == 1:
		reminderType = "due_tomorrow"
		text = formatDueTomorrowReminder(debt)
	case diffDays < 0:
		reminderType = "overdue"
		text = formatOverdueReminder(debt, -diffDays)
	default:
		return // due further out than tomorrow, not yet relevant
	}

	sent, err := s.reminderRepo.TryRecordReminder(debt.ID, reminderType, today)
	if err != nil {
		log.Printf("[DebtReminderScheduler] ERROR: failed to record reminder for debt %s: %v", debt.ID, err)
		return
	}
	if !sent {
		// A reminder was already recorded for this debt today -> skip to avoid duplicates.
		return
	}

	msg := tgbotapi.NewMessage(item.TelegramID, text)
	if _, err := s.bot.Send(msg); err != nil {
		log.Printf("[DebtReminderScheduler] ERROR: failed to send reminder to telegram_id %d: %v", item.TelegramID, err)
	}
}

func debtLabel(d *models.Debt) string {
	if d.Direction == "receivable" {
		return fmt.Sprintf("Piutang %s", d.PersonName)
	}
	return fmt.Sprintf("Hutang ke %s", d.PersonName)
}

func formatDueTodayReminder(d *models.Debt) string {
	return fmt.Sprintf("🔔 %s Rp%s jatuh tempo hari ini.", debtLabel(d), formatRupiahAmount(d.Amount-d.PaidAmount))
}

func formatDueTomorrowReminder(d *models.Debt) string {
	return fmt.Sprintf("⏰ %s Rp%s jatuh tempo besok.", debtLabel(d), formatRupiahAmount(d.Amount-d.PaidAmount))
}

func formatOverdueReminder(d *models.Debt, daysOverdue int) string {
	return fmt.Sprintf("⚠️ %s Rp%s sudah lewat jatuh tempo %d hari.", debtLabel(d), formatRupiahAmount(d.Amount-d.PaidAmount), daysOverdue)
}

// formatRupiahAmount groups an amount with thousand separators (e.g. 200000 -> "200.000").
func formatRupiahAmount(amount int64) string {
	if amount < 0 {
		amount = -amount
	}
	s := fmt.Sprintf("%d", amount)
	if len(s) <= 3 {
		return s
	}

	var out []byte
	n := 0
	for i := len(s) - 1; i >= 0; i-- {
		if n > 0 && n%3 == 0 {
			out = append([]byte{'.'}, out...)
		}
		out = append([]byte{s[i]}, out...)
		n++
	}
	return string(out)
}
