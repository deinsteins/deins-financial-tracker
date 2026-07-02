package handlers

import (
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"finance-bot/bot/models"
	"finance-bot/bot/services"
)

var networthKeywords = []string{
	"saldo", "tabungan", "rekening", "cash", "dompet",
	"saham", "crypto", "emas", "aset", "asset",
	"cicilan", "pinjaman", "liability", "liabilitas",
}

func containsNetWorthKeyword(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range networthKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func (h *BotHandler) tryHandleNetWorthIntent(msg *tgbotapi.Message, text string) bool {
	if !containsNetWorthKeyword(text) {
		return false
	}

	parsed, err := h.finance.ParseNetWorthText(text)
	if err != nil {
		log.Printf("WARNING: net worth intent parsing failed, falling back to normal parsing: %v", err)
		return false
	}

	replyText, handled := h.buildNetWorthReply(msg.From.ID, parsed)
	if !handled {
		return false
	}

	h.sendReply(msg.Chat.ID, replyText, msg.MessageID)

	_ = h.finance.SaveChatHistory(msg.From.ID, "user", msg.Text)
	_ = h.finance.SaveChatHistory(msg.From.ID, "assistant", replyText)
	return true
}

func (h *BotHandler) buildNetWorthReply(telegramID int64, parsed *services.NetWorthParseResponse) (string, bool) {
	switch parsed.Intent {
	case "add_asset":
		return h.networthIntentAddAsset(telegramID, parsed)
	case "update_asset":
		return h.networthIntentUpdateAsset(telegramID, parsed)
	case "delete_asset":
		return h.networthIntentDeleteAsset(telegramID, parsed)
	case "add_liability":
		return h.networthIntentAddLiability(telegramID, parsed)
	case "update_liability":
		return h.networthIntentUpdateLiability(telegramID, parsed)
	case "delete_liability":
		return h.networthIntentDeleteLiability(telegramID, parsed)
	case "show_networth":
		status, err := h.finance.GetNetWorthStatus(telegramID)
		if err != nil {
			return fmt.Sprintf("⚠️ *Gagal mengambil status net worth:*\n%v", err), true
		}
		return status, true
	default: // "unknown"
		if stringOrEmpty(parsed.Name) == "" {
			return "", false
		}
		reason := "Gua kurang yakin maksud lu apa nih bro."
		if r := stringOrEmpty(parsed.Reason); r != "" {
			reason = r
		}
		return fmt.Sprintf("🤔 *Kurang Paham Nih*\n\n%s\n\n"+
			"_Coba lebih spesifik ya, misal: \"saldo BCA saya 12 juta\" atau \"update saldo BCA jadi 15 juta\"._",
			reason), true
	}
}

func (h *BotHandler) networthIntentAddAsset(telegramID int64, parsed *services.NetWorthParseResponse) (string, bool) {
	name := stringOrEmpty(parsed.Name)
	assetType := stringOrEmpty(parsed.Type)
	if name == "" || assetType == "" || parsed.Amount == nil || *parsed.Amount <= 0 {
		return "⚠️ Info nama/tipe/jumlah aset kurang jelas nih bro.\n\n" +
			"_Coba lebih detail, misal: \"saldo BCA saya 12 juta\" atau \"cash di dompet 500rb\"._", true
	}

	notes := stringOrEmpty(parsed.Notes)
	asset, err := h.finance.AddAsset(telegramID, assetType, name, *parsed.Amount, notes)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal menambahkan aset:*\n%v", err), true
	}

	return fmt.Sprintf("✅ *Aset Berhasil Ditambahkan!* 🎉\n\n• *Nama:* %s\n• *Tipe:* %s\n• *Jumlah:* %s\n\n_Dicatat otomatis dari pesan lu ya bro!_ 👍",
		asset.Name, asset.AssetType, formatIDRCurrency(asset.Amount)), true
}

func (h *BotHandler) networthIntentUpdateAsset(telegramID int64, parsed *services.NetWorthParseResponse) (string, bool) {
	name := stringOrEmpty(parsed.Name)
	if name == "" || parsed.Amount == nil || *parsed.Amount <= 0 {
		return "⚠️ Info nama/jumlah aset kurang jelas nih bro.\n\n" +
			"_Coba lebih detail, misal: \"update saldo BCA jadi 15 juta\"._", true
	}

	assets, err := h.finance.GetAssets(telegramID)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mencari aset:*\n%v", err), true
	}

	var targetAsset *models.Asset
	for _, a := range assets {
		if strings.EqualFold(a.Name, name) {
			targetAsset = a
			break
		}
	}

	if targetAsset == nil {
		return fmt.Sprintf("⚠️ Aset dengan nama *%s* tidak ditemukan.", name), true
	}

	err = h.finance.UpdateAssetAmount(telegramID, targetAsset.ID, *parsed.Amount)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mengupdate aset:*\n%v", err), true
	}

	return fmt.Sprintf("✅ *Aset Berhasil Diupdate!* 📈\n\n• *Nama:* %s\n• *Jumlah Baru:* %s\n\n_Diupdate otomatis dari pesan lu ya bro!_ 👍",
		targetAsset.Name, formatIDRCurrency(*parsed.Amount)), true
}

func (h *BotHandler) networthIntentDeleteAsset(telegramID int64, parsed *services.NetWorthParseResponse) (string, bool) {
	name := stringOrEmpty(parsed.Name)
	if name == "" {
		return "⚠️ Nama aset yang ingin dihapus kurang jelas nih bro.\n\n" +
			"_Coba lebih detail, misal: \"hapus aset BCA\"._", true
	}

	assets, err := h.finance.GetAssets(telegramID)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mencari aset:*\n%v", err), true
	}

	var targetAsset *models.Asset
	for _, a := range assets {
		if strings.EqualFold(a.Name, name) {
			targetAsset = a
			break
		}
	}

	if targetAsset == nil {
		return fmt.Sprintf("⚠️ Aset dengan nama *%s* tidak ditemukan.", name), true
	}

	err = h.finance.DeleteAsset(telegramID, targetAsset.ID)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal menghapus aset:*\n%v", err), true
	}

	return fmt.Sprintf("✅ *Aset Berhasil Dihapus!* ❌\n\n• *Nama:* %s\n\n_Dihapus otomatis dari pesan lu ya bro!_ 👍", targetAsset.Name), true
}

func (h *BotHandler) networthIntentAddLiability(telegramID int64, parsed *services.NetWorthParseResponse) (string, bool) {
	name := stringOrEmpty(parsed.Name)
	liabilityType := stringOrEmpty(parsed.Type)
	if name == "" || liabilityType == "" || parsed.Amount == nil || *parsed.Amount <= 0 {
		return "⚠️ Info nama/tipe/jumlah kewajiban kurang jelas nih bro.\n\n" +
			"_Coba lebih detail, misal: \"cicilan motor sisa 2 juta\"._", true
	}

	notes := stringOrEmpty(parsed.Notes)
	liability, err := h.finance.AddLiability(telegramID, liabilityType, name, *parsed.Amount, nil, notes)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal menambahkan kewajiban:*\n%v", err), true
	}

	return fmt.Sprintf("✅ *Kewajiban Berhasil Ditambahkan!* 🎉\n\n• *Nama:* %s\n• *Tipe:* %s\n• *Jumlah:* %s\n\n_Dicatat otomatis dari pesan lu ya bro!_ 👍",
		liability.Name, liability.LiabilityType, formatIDRCurrency(liability.Amount)), true
}

func (h *BotHandler) networthIntentUpdateLiability(telegramID int64, parsed *services.NetWorthParseResponse) (string, bool) {
	name := stringOrEmpty(parsed.Name)
	if name == "" || parsed.Amount == nil || *parsed.Amount <= 0 {
		return "⚠️ Info nama/jumlah kewajiban kurang jelas nih bro.\n\n" +
			"_Coba lebih detail, misal: \"update cicilan motor jadi 1.5 juta\"._", true
	}

	liabilities, err := h.finance.GetLiabilities(telegramID)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mencari kewajiban:*\n%v", err), true
	}

	var targetLiab *models.Liability
	for _, l := range liabilities {
		if strings.EqualFold(l.Name, name) {
			targetLiab = l
			break
		}
	}

	if targetLiab == nil {
		return fmt.Sprintf("⚠️ Kewajiban dengan nama *%s* tidak ditemukan.", name), true
	}

	err = h.finance.UpdateLiabilityAmount(telegramID, targetLiab.ID, *parsed.Amount)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mengupdate kewajiban:*\n%v", err), true
	}

	return fmt.Sprintf("✅ *Kewajiban Berhasil Diupdate!* 📈\n\n• *Nama:* %s\n• *Jumlah Baru:* %s\n\n_Diupdate otomatis dari pesan lu ya bro!_ 👍",
		targetLiab.Name, formatIDRCurrency(*parsed.Amount)), true
}

func (h *BotHandler) networthIntentDeleteLiability(telegramID int64, parsed *services.NetWorthParseResponse) (string, bool) {
	name := stringOrEmpty(parsed.Name)
	if name == "" {
		return "⚠️ Nama kewajiban yang ingin dihapus kurang jelas nih bro.\n\n" +
			"_Coba lebih detail, misal: \"hapus kewajiban cicilan motor\"._", true
	}

	liabilities, err := h.finance.GetLiabilities(telegramID)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal mencari kewajiban:*\n%v", err), true
	}

	var targetLiab *models.Liability
	for _, l := range liabilities {
		if strings.EqualFold(l.Name, name) {
			targetLiab = l
			break
		}
	}

	if targetLiab == nil {
		return fmt.Sprintf("⚠️ Kewajiban dengan nama *%s* tidak ditemukan.", name), true
	}

	err = h.finance.DeleteLiability(telegramID, targetLiab.ID)
	if err != nil {
		return fmt.Sprintf("⚠️ *Gagal menghapus kewajiban:*\n%v", err), true
	}

	return fmt.Sprintf("✅ *Kewajiban Berhasil Dihapus!* ❌\n\n• *Nama:* %s\n\n_Dihapus otomatis dari pesan lu ya bro!_ 👍", targetLiab.Name), true
}
