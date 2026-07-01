package scheduler

import (
	"testing"
	"time"

	"finance-bot/bot/models"
	"finance-bot/bot/repositories"
)

func TestFormatDueTodayReminder(t *testing.T) {
	d := &models.Debt{PersonName: "Andi", Direction: "receivable", Amount: 200000}
	got := formatDueTodayReminder(d)
	want := "🔔 Piutang Andi Rp200.000 jatuh tempo hari ini."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatDueTomorrowReminder(t *testing.T) {
	d := &models.Debt{PersonName: "Budi", Direction: "payable", Amount: 500000}
	got := formatDueTomorrowReminder(d)
	want := "⏰ Hutang ke Budi Rp500.000 jatuh tempo besok."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatOverdueReminder(t *testing.T) {
	d := &models.Debt{PersonName: "Dina", Direction: "receivable", Amount: 75000}
	got := formatOverdueReminder(d, 3)
	want := "⚠️ Piutang Dina Rp75.000 sudah lewat jatuh tempo 3 hari."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatReminders_UseRemainingAmount(t *testing.T) {
	d := &models.Debt{PersonName: "Andi", Direction: "receivable", Amount: 200000, PaidAmount: 50000}
	got := formatDueTodayReminder(d)
	want := "🔔 Piutang Andi Rp150.000 jatuh tempo hari ini."
	if got != want {
		t.Errorf("got %q, want %q (should use remaining amount, not original)", got, want)
	}
}

func TestFormatRupiahAmount(t *testing.T) {
	cases := map[int64]string{
		0:       "0",
		999:     "999",
		1000:    "1.000",
		200000:  "200.000",
		1234567: "1.234.567",
		-500000: "500.000",
	}
	for amount, want := range cases {
		if got := formatRupiahAmount(amount); got != want {
			t.Errorf("formatRupiahAmount(%d) = %q, want %q", amount, got, want)
		}
	}
}

func TestNextRunTime_BeforeEightAM(t *testing.T) {
	now := time.Date(2026, 7, 1, 6, 0, 0, 0, time.UTC)
	got := nextRunTime(now)
	want := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_AfterEightAM(t *testing.T) {
	now := time.Date(2026, 7, 1, 9, 30, 0, 0, time.UTC)
	got := nextRunTime(now)
	want := time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextRunTime_ExactlyEightAM(t *testing.T) {
	now := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	got := nextRunTime(now)
	want := time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// fakeReminderRepo lets tests observe whether/how TryRecordReminder was
// called without touching a real database. When alreadySent is true it
// simulates a reminder already recorded today (RowsAffected == 0), which
// also conveniently prevents processDebt from reaching bot.Send with a nil
// *tgbotapi.BotAPI in these unit tests.
type fakeReminderRepo struct {
	called      bool
	lastDebtID  string
	lastType    string
	alreadySent bool
}

func (f *fakeReminderRepo) TryRecordReminder(debtID, reminderType string, reminderDate time.Time) (bool, error) {
	f.called = true
	f.lastDebtID = debtID
	f.lastType = reminderType
	if f.alreadySent {
		return false, nil
	}
	return true, nil
}

func TestProcessDebt_DueDateClassification(t *testing.T) {
	today := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name        string
		dueDate     time.Time
		wantHandled bool
		wantType    string
	}{
		{"due today", today, true, "due_today"},
		{"due tomorrow", today.AddDate(0, 0, 1), true, "due_tomorrow"},
		{"overdue by 3 days", today.AddDate(0, 0, -3), true, "overdue"},
		{"due in 2 days (not yet relevant)", today.AddDate(0, 0, 2), false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// alreadySent=true so processDebt never reaches the real
			// bot.Send call (s.bot is nil in this unit test).
			fakeRepo := &fakeReminderRepo{alreadySent: true}
			s := &DebtReminderScheduler{reminderRepo: fakeRepo}
			dueDate := tc.dueDate
			debt := &models.Debt{ID: "debt-1", PersonName: "Andi", Direction: "receivable", Amount: 100000, DueDate: &dueDate}

			s.processDebt(&repositories.DueDebt{Debt: debt, TelegramID: 12345}, today)

			if tc.wantHandled && !fakeRepo.called {
				t.Errorf("expected TryRecordReminder to be called, but it wasn't")
			}
			if !tc.wantHandled && fakeRepo.called {
				t.Errorf("expected TryRecordReminder not to be called, but it was")
			}
			if tc.wantHandled && fakeRepo.lastType != tc.wantType {
				t.Errorf("got reminder type %q, want %q", fakeRepo.lastType, tc.wantType)
			}
		})
	}
}

func TestProcessDebt_SkipsDuplicateReminder(t *testing.T) {
	today := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	fakeRepo := &fakeReminderRepo{alreadySent: true}
	s := &DebtReminderScheduler{reminderRepo: fakeRepo}
	debt := &models.Debt{ID: "debt-1", PersonName: "Andi", Direction: "receivable", Amount: 100000, DueDate: &today}

	// s.bot is nil; if processDebt tried to send a message despite the
	// "already sent" result, this would panic on the nil pointer. Reaching
	// the end without panicking confirms the duplicate was correctly skipped.
	s.processDebt(&repositories.DueDebt{Debt: debt, TelegramID: 12345}, today)

	if !fakeRepo.called {
		t.Fatalf("expected TryRecordReminder to be called")
	}
}
