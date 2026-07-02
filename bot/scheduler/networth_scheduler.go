package scheduler

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"finance-bot/bot/models"
	"finance-bot/bot/repositories"
)

type NetWorthScheduler struct {
	bot          *tgbotapi.BotAPI
	userRepo     repositories.UserRepository
	netWorthRepo repositories.NetWorthRepository
	debtRepo     repositories.DebtRepository
	loc          *time.Location
}

func NewNetWorthScheduler(
	bot *tgbotapi.BotAPI,
	userRepo repositories.UserRepository,
	netWorthRepo repositories.NetWorthRepository,
	debtRepo repositories.DebtRepository,
) *NetWorthScheduler {
	tz := os.Getenv("TZ")
	if tz == "" {
		tz = "Asia/Jakarta"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}

	return &NetWorthScheduler{
		bot:          bot,
		userRepo:     userRepo,
		netWorthRepo: netWorthRepo,
		debtRepo:     debtRepo,
		loc:          loc,
	}
}

func (s *NetWorthScheduler) Start() {
	go s.run()
}

func (s *NetWorthScheduler) run() {
	for {
		next := nextMonthlyRunTime(time.Now().In(s.loc))
		wait := time.Until(next)
		log.Printf("[NetWorthScheduler] next monthly run at %s (in %s)", next.Format(time.RFC3339), wait.Round(time.Second))
		time.Sleep(wait)
		s.RunOnce()
	}
}

func nextMonthlyRunTime(now time.Time) time.Time {
	// First day of current month at 08:00
	next := time.Date(now.Year(), now.Month(), 1, 8, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 1, 0)
	}
	return next
}

func (s *NetWorthScheduler) RunOnce() {
	users, err := s.userRepo.GetAllUsers()
	if err != nil {
		log.Printf("[NetWorthScheduler] ERROR: failed to fetch all users: %v", err)
		return
	}

	log.Printf("[NetWorthScheduler] Running monthly snapshots for %d user(s)", len(users))
	for _, user := range users {
		s.processUserNetWorth(user)
	}
}

func (s *NetWorthScheduler) processUserNetWorth(user *models.User) {
	// 1. Calculate assets & liabilities
	assets, err := s.netWorthRepo.GetAssetsByUser(user.ID)
	if err != nil {
		log.Printf("[NetWorthScheduler] ERROR: failed to fetch assets for user %s: %v", user.ID, err)
		return
	}

	liabilities, err := s.netWorthRepo.GetLiabilitiesByUser(user.ID)
	if err != nil {
		log.Printf("[NetWorthScheduler] ERROR: failed to fetch liabilities for user %s: %v", user.ID, err)
		return
	}

	activeDebts, err := s.debtRepo.GetActiveDebtsByUser(user.ID)
	if err != nil {
		log.Printf("[NetWorthScheduler] ERROR: failed to fetch active debts for user %s: %v", user.ID, err)
		return
	}

	var manualAssets, manualLiabilities int64
	for _, a := range assets {
		manualAssets += a.Amount
	}
	for _, l := range liabilities {
		manualLiabilities += l.Amount
	}

	var totalReceivables, totalPayables int64
	for _, d := range activeDebts {
		remaining := d.Amount - d.PaidAmount
		if remaining <= 0 {
			continue
		}
		if d.Direction == "receivable" {
			totalReceivables += remaining
		} else {
			totalPayables += remaining
		}
	}

	totalAssets := manualAssets + totalReceivables
	totalLiabilities := manualLiabilities + totalPayables
	netWorth := totalAssets - totalLiabilities

	// 2. Fetch history before inserting/upserting new snapshot
	history, err := s.netWorthRepo.GetNetWorthHistory(user.ID)
	if err != nil {
		log.Printf("[NetWorthScheduler] ERROR: failed to fetch net worth history for user %s: %v", user.ID, err)
		return
	}

	// 3. Upsert snapshot
	today := time.Now().In(s.loc).Truncate(24 * time.Hour)
	snapshot := &models.NetWorthSnapshot{
		UserID:           user.ID,
		TotalAssets:      totalAssets,
		TotalLiabilities: totalLiabilities,
		NetWorth:         netWorth,
		SnapshotDate:     today,
	}

	err = s.netWorthRepo.CreateNetWorthSnapshot(snapshot)
	if err != nil {
		log.Printf("[NetWorthScheduler] ERROR: failed to create snapshot for user %s: %v", user.ID, err)
		return
	}

	// 4. Find previous month's snapshot (if any) from history.
	var previousSnapshot *models.NetWorthSnapshot
	for i := len(history) - 1; i >= 0; i-- {
		hSnap := history[i]
		if hSnap.SnapshotDate.Before(today) {
			previousSnapshot = hSnap
			break
		}
	}

	// 5. Format message
	var msgText strings.Builder
	msgText.WriteString("📈 *Net Worth Bulanan*\n\n")
	msgText.WriteString(fmt.Sprintf("Net worth kamu per %d %s:\n%s\n\n",
		today.Day(), translateMonthID(today.Month()), formatRupiahWithPrefix(netWorth)))

	if previousSnapshot != nil {
		diff := netWorth - previousSnapshot.NetWorth
		diffStr := formatRupiahWithPrefix(diff)
		if diff >= 0 {
			diffStr = "+" + diffStr
		} else {
			diffStr = "-" + diffStr
		}

		msgText.WriteString(fmt.Sprintf("Perubahan dari bulan lalu:\n*%s*\n\n", diffStr))

		assetDiff := totalAssets - previousSnapshot.TotalAssets
		liabDiff := totalLiabilities - previousSnapshot.TotalLiabilities

		var reasons []string
		if assetDiff > 0 {
			reasons = append(reasons, "saldo aset atau piutang meningkat")
		} else if assetDiff < 0 {
			reasons = append(reasons, "saldo aset atau piutang menurun")
		}

		if liabDiff > 0 {
			reasons = append(reasons, "kewajiban atau hutang bertambah")
		} else if liabDiff < 0 {
			reasons = append(reasons, "kewajiban atau hutang berkurang")
		}

		if len(reasons) > 0 {
			msgText.WriteString(fmt.Sprintf("Aset/Kewajiban berubah karena %s.", strings.Join(reasons, " dan ")))
		} else {
			msgText.WriteString("Tidak ada perubahan saldo aset dan kewajiban dibanding bulan lalu.")
		}
	} else {
		msgText.WriteString("Ini adalah catatan net worth bulanan pertamamu!")
	}

	// 6. Send Telegram message
	tgMsg := tgbotapi.NewMessage(user.TelegramID, msgText.String())
	tgMsg.ParseMode = "Markdown"
	if _, err := s.bot.Send(tgMsg); err != nil {
		log.Printf("[NetWorthScheduler] ERROR: failed to send monthly net worth message to telegram_id %d: %v", user.TelegramID, err)
	}
}

func translateMonthID(m time.Month) string {
	months := map[time.Month]string{
		time.January:   "Januari",
		time.February:  "Februari",
		time.March:     "Maret",
		time.April:     "April",
		time.May:       "Mei",
		time.June:      "Juni",
		time.July:      "Juli",
		time.August:    "Agustus",
		time.September: "September",
		time.October:   "Oktober",
		time.November:  "November",
		time.December:  "Desember",
	}
	return months[m]
}

// formatRupiahWithPrefix groups an amount with thousand separators (e.g. 200000 -> "Rp200.000").
func formatRupiahWithPrefix(amount int64) string {
	negative := amount < 0
	if negative {
		amount = -amount
	}
	s := fmt.Sprintf("%d", amount)
	if len(s) <= 3 {
		if negative {
			return "-Rp" + s
		}
		return "Rp" + s
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
	res := "Rp" + string(out)
	if negative {
		res = "-Rp" + string(out)
	}
	return res
}
