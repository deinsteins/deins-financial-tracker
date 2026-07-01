# Plan: Extend OCR Pipeline with Structured Receipt Extraction

## Goal

Extend the existing `POST /ocr` endpoint to send Tesseract raw text through Gemini (or custom LLM) for structured receipt extraction. Return merchant, items, total, and date as validated JSON.

## Decisions

| Decision | Choice |
|---|---|
| Endpoint | Modify existing `POST /ocr` (no new route) |
| Item fields | `name`, `qty`, `price` per item |
| No-LLM fallback | 503 error with raw OCR text in body |
| Monetary values | `int` (IDR, no decimals) — consistent with `ParsedTransaction.amount` |
| Date format | ISO 8601 string, `null` when not found on receipt |
| Currency normalization | Apply `normalize_indonesian_currency()` to raw OCR text before sending to LLM |
| Code location | New `parse_receipt()` method in `ParserService` |

## Response Schema

```python
class ReceiptItem(BaseModel):
    name: str              # e.g. "Bakso Spesial"
    qty: int               # default 1 when not on receipt
    price: int             # unit price in IDR

class OCRReceiptResponse(BaseModel):
    filename: str          # original upload filename
    raw_text: str          # Tesseract raw output
    merchant: str          # store/restaurant name
    items: list[ReceiptItem]
    total: int             # normalized total in IDR
    date: str | None       # ISO 8601 or null
```

## Data Flow

```
Upload image
  -> validate file type/size (existing logic, unchanged)
  -> Tesseract OCR -> raw_text
  -> normalize_indonesian_currency(raw_text) -> normalized_text
  -> ParserService.parse_receipt(normalized_text)
       -> Gemini SDK (preferred) or custom LLM
       -> parse JSON response
       -> validate with Pydantic (ReceiptItem / receipt dict)
       -> return structured data
  -> assemble OCRReceiptResponse(filename, raw_text, **structured_data)
```

## Tasks

### 1. Add Pydantic models in `parser_service.py`

- Define `ReceiptItem(BaseModel)` with `name: str`, `qty: int`, `price: int`.
- Define `ParsedReceipt(BaseModel)` with `merchant: str`, `items: list[ReceiptItem]`, `total: int`, `date: str | None`.

### 2. Add `parse_receipt()` method to `ParserService` in `parser_service.py`

- Accept `text: str` (already currency-normalized).
- Follow the existing Gemini -> custom LLM priority chain (same as `parse_transaction()`).
- Prompt Gemini to extract `merchant`, `items` (each with `name`, `qty`, `price`), `total`, and `date` from OCR text.
- Instruct the prompt to:
  - Return all monetary values as plain integers in IDR (no "Rp", no dots).
  - Default `qty` to 1 when not visible.
  - Return `date` in ISO 8601 format, or `null` if not found.
  - Return ONLY raw JSON, no markdown wrappers.
- Parse and validate the LLM response with `ParsedReceipt`.
- If no LLM is configured, raise a `ValueError` with a message indicating LLM is required.

### 3. Update `POST /ocr` endpoint in `main.py`

- Replace `OCRResponse` with `OCRReceiptResponse` (add `merchant`, `items`, `total`, `date` fields alongside existing `filename` and `raw_text`/`text`).
- After OCR extraction, apply `normalize_indonesian_currency()` to the raw text.
- Call `parser_service.parse_receipt(normalized_text)`.
- Catch `ValueError` from missing LLM config and return HTTP 503 with `{"detail": "...", "raw_text": "..."}`.
- Catch general LLM/parsing exceptions and return HTTP 500.
- Assemble the response from OCR raw text + parsed receipt fields.

### 4. Import updates in `main.py`

- Import `normalize_indonesian_currency` from `parser_service`.
- Import new Pydantic models if used directly (or keep them internal to the service).

## Error Handling

| Scenario | HTTP Status | Detail |
|---|---|---|
| No LLM configured | 503 | "Receipt extraction requires a configured LLM. Raw OCR text is included." + `raw_text` |
| LLM returns unparseable JSON | 500 | "Failed to parse receipt data from LLM response" |
| LLM API call fails | 500 | "Failed to extract receipt data: {error}" |
| Pydantic validation fails on LLM output | 500 | "LLM returned invalid receipt structure: {error}" |

## Validation

- Upload a real Indonesian receipt photo and confirm structured fields match.
- Upload an image with no text — expect empty/minimal structured output.
- Disable `GEMINI_API_KEY` — expect 503 with raw OCR text in body.
- Verify all monetary values are integers, not strings.
- Verify `date` is ISO 8601 or null.

## Risks

- **OCR quality**: Tesseract on low-quality receipt photos may produce garbled text. Gemini should still attempt best-effort extraction — the prompt should instruct it to use empty/null for unreadable fields rather than hallucinating.
- **Gemini rate limits**: Same risk as existing `/parse` and `/analyze`. No new mitigation needed beyond what exists.
