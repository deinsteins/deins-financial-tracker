# Plan: Receipt Confirmation Flow

## Goal

After OCR parses a receipt photo, show the user a confirmation prompt with inline keyboard buttons. Support confirming (saves as transaction), editing the amount, or discarding.

## Decisions

| Decision | Choice |
|---|---|
| Pending state storage | In-memory `map[int64]*PendingReceipt` with `sync.Mutex`, keyed by chatID |
| TTL for pending receipts | 5 minutes, stale entries cleaned on access |
| Edit amount UX | Text reply — bot asks "Ketik jumlah baru:", next text message parsed via `parseAmount()` |
| Default category | `"food"` (receipt scanning is overwhelmingly food/warung) |
| Default wallet | `"cash"` |
| Transaction type | Always `"expense"` |
| Description format | `"Merchant (N items)"` or `"Merchant"` if no items |

## UX Flow

```
1. User sends photo → OCR processes → bot shows receipt summary
   with inline keyboard: [ Simpan ✅ ] [ Edit Jumlah ✏️ ] [ Batal ❌ ]

2a. User taps "Simpan ✅"
    → Save transaction via AddTransaction(telegramID, "expense", "food", total, desc, "cash")
    → Reply with success card (same format as save_transaction tool response)
    → Include budget alert check for "food" category
    → Clear pending state

2b. User taps "Edit Jumlah ✏️"
    → Bot replies "Ketik jumlah baru (contoh: 25rb, 50000):"
    → Set pendingReceipt.AwaitingEdit = true
    → Next text message from this chatID:
      - Parse via parseAmount()
      - Update pendingReceipt.Total
      - Re-send receipt summary with updated total + same 3 buttons
      - Set AwaitingEdit = false

2c. User taps "Batal ❌"
    → Delete pending state
    → Reply "Oke, struk dibatalkan! 👍"

2d. Button pressed after TTL expired
    → Reply "Struk sudah expired, kirim ulang foto ya bro!"
```

## Callback Data Encoding

Telegram limits `callback_data` to 64 bytes. Use short prefixes:

- `ocr:confirm` — save the pending receipt
- `ocr:edit` — enter edit-amount mode
- `ocr:cancel` — discard

No receipt data in the callback payload — the chatID is the lookup key into the pending map.

## Data Structures

### PendingReceipt (in `bot_handler.go`)

```go
type PendingReceipt struct {
    ChatID       int64
    TelegramID   int64     // user's Telegram ID for AddTransaction
    MessageID    int        // original message ID for reply threading
    Merchant     string
    Items        []services.OCRReceiptItem
    Total        int64
    Date         *string
    RawText      string
    AwaitingEdit bool       // true when waiting for user to type new amount
    CreatedAt    time.Time  // for TTL expiry
}
```

### Pending Store (in `bot_handler.go`)

```go
var (
    pendingReceipts   = make(map[int64]*PendingReceipt) // keyed by chatID
    pendingReceiptsMu sync.Mutex
)

const pendingReceiptTTL = 5 * time.Minute
```

Helper functions:
- `setPendingReceipt(chatID int64, pr *PendingReceipt)` — stores, cleans expired entries
- `getPendingReceipt(chatID int64) *PendingReceipt` — returns nil if not found or expired
- `deletePendingReceipt(chatID int64)` — removes entry

## Tasks

### 1. Add PendingReceipt struct and in-memory store to `bot/handlers/bot_handler.go`

- Add `sync` to imports.
- Define `PendingReceipt` struct with fields listed above.
- Define package-level `pendingReceipts` map, `pendingReceiptsMu` mutex, and `pendingReceiptTTL` constant.
- Implement `setPendingReceipt()`, `getPendingReceipt()`, `deletePendingReceipt()` helper functions.
- `setPendingReceipt` should also garbage-collect entries older than TTL on each call to prevent unbounded memory growth.

### 2. Update `HandleUpdates` to handle callback queries

Currently `HandleUpdates` skips when `update.Message == nil`. It must now also handle `update.CallbackQuery != nil`.

Updated routing logic:
```
if update.CallbackQuery != nil {
    h.handleCallback(update.CallbackQuery)
    continue
}
if update.Message == nil {
    continue
}
// ... existing command/photo/text routing
```

### 3. Add text message intercept for edit-amount flow

At the top of `handleTextMessage`, before the existing orchestration logic, check if a pending receipt is awaiting edit:

```
pr := getPendingReceipt(msg.Chat.ID)
if pr != nil && pr.AwaitingEdit {
    h.handleReceiptAmountEdit(msg, pr)
    return
}
```

Implement `handleReceiptAmountEdit(msg, pr)`:
- Parse the text via `parseAmount(msg.Text)`
- If parse fails, reply with error and keep AwaitingEdit=true
- If parse succeeds, update `pr.Total`, set `pr.AwaitingEdit = false`, store updated pending receipt
- Re-send receipt summary with updated total + inline keyboard buttons

### 4. Update `handlePhotoMessage` to show inline keyboard instead of plain reply

After OCR succeeds, instead of sending a plain text reply:
- Store a `PendingReceipt` in the pending map
- Build the receipt summary text (reuse `formatReceiptSummary`)
- Append "\n_Simpan sebagai transaksi?_" to the summary
- Create an `InlineKeyboardMarkup` with 3 buttons:
  ```
  [ Simpan ✅ | callback_data: "ocr:confirm" ]
  [ Edit Jumlah ✏️ | callback_data: "ocr:edit" ]
  [ Batal ❌ | callback_data: "ocr:cancel" ]
  ```
- Send the message with `msg.ReplyMarkup = keyboard`

### 5. Implement `handleCallback(cq *tgbotapi.CallbackQuery)`

- Always call `bot.Request(tgbotapi.NewCallback(cq.ID, ""))` to acknowledge the callback (stops the Telegram loading spinner).
- Extract `chatID` from `cq.Message.Chat.ID`.
- Look up `getPendingReceipt(chatID)`.
- If nil (expired or not found): answer with "Struk sudah expired, kirim ulang foto ya bro!" and return.
- Switch on `cq.Data`:

**`"ocr:confirm"`**:
- Call `h.finance.AddTransaction(pr.TelegramID, "expense", "food", pr.Total, description, "cash")`.
  - Build description: if merchant is non-empty, use `"Merchant (N items)"` format; otherwise `"Scan struk (N items)"`.
- Format success card matching existing `save_transaction` format:
  ```
  ✅ *Catatan Berhasil Disimpan!* 🎉

  • *Tipe*: 💸 pengeluaran
  • *Kategori*: food
  • *Jumlah*: Rp 25.000
  • *Dompet*: cash
  • *Deskripsi*: Bakso Pak Kumis (3 items)
  ```
- Check budget alerts via `h.finance.CheckBudgetAlerts(pr.TelegramID, "food")` and append if present.
- Edit the original message to remove the inline keyboard (replace with the success text) using `tgbotapi.NewEditMessageText`.
- Delete pending receipt.
- Save chat history.

**`"ocr:edit"`**:
- Set `pr.AwaitingEdit = true`, store back.
- Reply: "✏️ _Ketik jumlah baru (contoh: 25rb, 50000):_"

**`"ocr:cancel"`**:
- Delete pending receipt.
- Edit the original message to show "❌ _Struk dibatalkan._" and remove the keyboard.

### 6. Add a `sendReplyWithKeyboard` helper method

Similar to existing `sendReply`, but accepts an `InlineKeyboardMarkup`:

```go
func (h *BotHandler) sendReplyWithKeyboard(chatID int64, text string, replyToMessageID int, keyboard tgbotapi.InlineKeyboardMarkup) {
    reply := tgbotapi.NewMessage(chatID, text)
    reply.ReplyToMessageID = replyToMessageID
    reply.ParseMode = "markdown"
    reply.ReplyMarkup = keyboard
    if _, err := h.bot.Send(reply); err != nil {
        log.Printf("Failed to send message with keyboard: %v", err)
    }
}
```

### 7. Build inline keyboard helper

```go
func receiptConfirmKeyboard() tgbotapi.InlineKeyboardMarkup {
    return tgbotapi.NewInlineKeyboardMarkup(
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("Simpan ✅", "ocr:confirm"),
            tgbotapi.NewInlineKeyboardButtonData("Edit Jumlah ✏️", "ocr:edit"),
            tgbotapi.NewInlineKeyboardButtonData("Batal ❌", "ocr:cancel"),
        ),
    )
}
```

## Files Changed

Only `bot/handlers/bot_handler.go` — no changes to services, models, AI client, or FastAPI.

## Edge Cases

| Scenario | Behavior |
|---|---|
| User sends new photo while old receipt is pending | Old pending receipt is overwritten by new one |
| User types random text while AwaitingEdit | `parseAmount()` fails, bot replies with error, keeps waiting |
| User sends command while AwaitingEdit | Commands are handled first (before text intercept), so `/today` etc. still work. The pending receipt stays until TTL. |
| User taps button twice quickly | Second tap finds no pending receipt (deleted on first tap), gets "expired" message |
| Bot restarts | Pending map is lost; stale buttons get "expired" response |
| OCR total is 0 | Still shows confirmation; user can edit amount before saving |

## Validation

- Send a receipt photo → see summary + 3 buttons
- Tap "Simpan" → transaction saved, success card shown, buttons removed
- Send another receipt → tap "Edit Jumlah" → type "30rb" → see updated total → tap "Simpan"
- Send receipt → tap "Batal" → message updated to "Struk dibatalkan"
- Send receipt → wait 5+ minutes → tap any button → get "expired" message
- Type random text during edit mode → see error, retry
- Send new photo while old receipt pending → old receipt replaced
