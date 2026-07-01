# Plan: Automatic Category Inference from Receipt

## Goal

Replace the hardcoded `"food"` category in the receipt confirmation flow with a category inferred from the receipt's merchant and items. Extend the canonical category enum to include `groceries` and `shopping`. Surface the inferred category in the pre-save summary so the user sees it before tapping Simpan.

## Decisions

| Decision | Choice |
|---|---|
| Extend canonical enum | Yes — add `groceries` and `shopping` |
| Inference location | Python `ai-service`, inside existing `parse_receipt()` Gemini call |
| Invalid-category handling | Coerce to `"other"` at both Python (Pydantic validator) and Go (sanitizer) boundaries |
| Confirmation UX | Show `🏷️ Kategori: <cat>` line in the summary; no edit-category button (keep 3 buttons) |
| DB migration | None — `transactions.category` is a free-text string |

## New Canonical Category Set

`food, groceries, shopping, transport, utilities, entertainment, salary, other`

Applied consistently everywhere the enum is enforced.

## Data Flow

```
User sends receipt photo
  → bot downloads image, POSTs to ai-service /ocr
  → parse_receipt() Gemini call returns merchant + items + total + date + CATEGORY
       (Pydantic validator coerces unknown → "other")
  → Go bot receives OCRReceiptResponse with Category field
  → sanitizeCategory() double-checks; unknown → "other"
  → PendingReceipt.Category set
  → formatReceiptSummary shows "🏷️ Kategori: groceries" line
  → user taps Simpan
  → AddTransaction(..., "expense", pr.Category, ...)
  → CheckBudgetAlerts uses pr.Category
```

## Tasks

### 1. `ai-service/parser_service.py`

- Add `category: str = Field(default="other", ...)` to `ParsedReceipt` model
- Add a Pydantic `field_validator` on `category` that lowercases input and coerces any value not in the canonical set to `"other"`
- Extend `parse_receipt()` prompt to include a "5. category" section listing the full enum and these concrete examples:
  - Indomaret / Alfamart / supermarket → `groceries`
  - Shell / Pertamina / gas station / gojek / grab → `transport`
  - Starbucks / warteg / restoran / cafe → `food`
  - Tokopedia / Shopee / mall / clothing store → `shopping`
  - Instruct Gemini to use merchant AND item names together
- Update `fallback_parse()` category branches to add `groceries` and `shopping` keyword sets:
  - `groceries`: `["indomaret", "alfamart", "supermarket", "belanja bulanan"]`
  - `shopping`: `["tokopedia", "shopee", "lazada", "mall", "belanja"]`
- Update category description strings in the `/parse` and `/analyze` Gemini prompts (lines currently reading `"food", "transport", "utilities", "entertainment", "salary", "other"`) to include `groceries` and `shopping`

### 2. `ai-service/main.py`

- Add `category: str = Field(default="other", example="groceries")` to `OCRReceiptResponse`
- Update the `parse_receipt` call site (in the `/ocr` endpoint) to pass `receipt.category` into `OCRReceiptResponse(...)`

### 3. `bot/services/ai_client.go`

- Add `Category string \`json:"category"\`` to the `OCRReceiptResponse` Go struct so the JSON field is decoded

### 4. `bot/handlers/bot_handler.go`

- Add `Category string` field to `PendingReceipt` struct
- In `handlePhotoMessage`, populate `PendingReceipt.Category` from `ocrResult.Category` (routed through `sanitizeCategory`)
- Add `sanitizeCategory(cat string) string` helper: lowercases input, returns `"other"` if not in the canonical set
- Extend `formatReceiptSummary` to render one line: `🏷️ *Kategori:* <category>` before the `💰 Total` line
  - Show even if empty — display `other` if unset, so users know they can edit later
- In `handleCallback` "ocr:confirm" branch (bot_handler.go:502):
  - Replace `"food"` with `sanitizeCategory(pr.Category)`
  - Update the success-card `• *Kategori*: food` line to use the same value
- In the budget-alert lookup (bot_handler.go:522):
  - Replace `"food"` with `sanitizeCategory(pr.Category)`

### 5. `bot/llm/tools.go`

- In `SaveTransactionTool.Parameters()` (line ~47): extend `Enum` array to `[]string{"food", "groceries", "shopping", "transport", "utilities", "entertainment", "salary", "other"}` and update the `Description` string to list the new items
- In `SaveTransactionTool.Validate()` (line ~74): add `"groceries": true, "shopping": true` to the valid-category map
- Update the error message on line ~78 to list the new enum
- Do the same three edits inside `SetCategoryBudgetTool` (lines ~333, ~352, ~356)

## Files Not Changed

- `bot/services/services.go` — no signature change to `AddTransaction`; it already accepts any string
- `bot/models/models.go` — no model change; `Transaction.Category` stays a free-text string
- No DB migration
- No changes to `/parse` or `/analyze` endpoint logic (only prompt strings)

## Edge Cases

| Scenario | Behavior |
|---|---|
| Gemini returns `"grocery"` (singular) | Pydantic validator coerces to `"other"` (only exact enum matches allowed); Go sanitizer also coerces defensively |
| Gemini returns `"Food"` (capitalized) | Both validators lowercase first, then check enum — accepted as `"food"` |
| Unknown merchant, no strong item signal | Gemini picks `"other"`; user sees it in summary, still saves cleanly |
| User has existing budget for a new category | Existing budget rows work as-is because DB stores free-text categories |
| Old transactions with only the original 6 categories | Unaffected — no migration or backfill needed |
| Gemini omits the `category` field entirely | Pydantic default `"other"` kicks in |
| Legacy `save_transaction` LLM tool call with `"groceries"` | Now accepted after the enum extension in `tools.go` |

## Validation

Manual checks after implementation:

1. Send Indomaret receipt photo → summary shows `🏷️ Kategori: groceries` → tap Simpan → success card shows `Kategori: groceries` and transaction is saved with `category=groceries`
2. Send Shell/Pertamina receipt → summary shows `transport`
3. Send Starbucks receipt → summary shows `food`
4. Send Tokopedia order screenshot → summary shows `shopping`
5. Send a receipt from an unknown small warung → summary shows `food` or `other` (LLM's call); no crash
6. Type `belanja di Alfamart 100rb` (text flow, not photo) → the `/parse` prompt update lets Hermes classify as `groceries`
7. `/budget set groceries 500rb` → succeeds (was previously rejected by tool validator)
8. Existing food/transport transactions still work end-to-end

## Risks

- **Prompt drift**: adding two categories widens the model's decision space slightly; Gemini may occasionally misclassify borderline cases (e.g., 7-Eleven → `food` vs `groceries`). Acceptable — user can correct later.
- **Enum synchronization**: the canonical set now lives in three places (Python prompt + fallback, Go tools.go, Go sanitizer). All three must be kept in sync. Consider extracting to a constant in a follow-up, but not in scope here.
- **Chat-history budget alerts**: users with existing food budgets who now scan a receipt classified as `groceries` won't trigger their food budget alert. Documented behavior, not a bug.

## Out of Scope

- Extracting the canonical enum into a shared constant/config file
- Adding an "Edit Kategori" button to the receipt confirmation UX
- Backfilling old transactions to the new category taxonomy
- Multilingual category names or user-defined categories
