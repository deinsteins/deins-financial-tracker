import os
import re
import json
import logging
import calendar
import urllib.request
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo
from pydantic import BaseModel, Field, field_validator
import google.generativeai as genai

logger = logging.getLogger(__name__)

VALID_CATEGORIES = {"food", "groceries", "shopping", "transport", "utilities", "entertainment", "salary", "other"}

# Pydantic schema for parsing validation
class ParsedTransaction(BaseModel):
    type: str = Field(..., description="Must be 'expense' or 'income'")
    category: str = Field(..., description="Normalized category, e.g., 'food', 'transport', 'utilities', 'entertainment', 'salary', 'other'")
    amount: int = Field(..., description="Transaction amount as an integer")
    description: str = Field(..., description="Sanitized description of the transaction")

class ReceiptItem(BaseModel):
    name: str = Field(..., description="Item name as printed on the receipt")
    qty: int = Field(..., description="Quantity purchased, defaults to 1")
    price: int = Field(..., description="Unit price in IDR as an integer")

class ParsedReceipt(BaseModel):
    merchant: str = Field(..., description="Store or restaurant name")
    items: list[ReceiptItem] = Field(..., description="List of purchased items")
    total: int = Field(..., description="Total amount in IDR as an integer")
    date: str | None = Field(..., description="ISO 8601 date string or null if not found")
    category: str = Field(..., description="Inferred transaction category")

    @field_validator("category", mode="before")
    @classmethod
    def validate_category(cls, v: str) -> str:
        if not isinstance(v, str):
            return "other"
        v = v.strip().lower()
        return v if v in VALID_CATEGORIES else "other"

VALID_DEBT_INTENTS = {
    "add_debt", "pay_debt", "mark_paid", "cancel_debt",
    "list_debt", "debt_summary", "unknown",
}
VALID_DEBT_DIRECTIONS = {"receivable", "payable"}

JAKARTA_TZ = ZoneInfo("Asia/Jakarta")

def clean_gemini_schema(pydantic_model) -> dict:
    schema = pydantic_model.model_json_schema()
    
    def clean_dict(d):
        if not isinstance(d, dict):
            return
        d.pop("default", None)
        d.pop("title", None)
        
        if "anyOf" in d:
            any_of = d.pop("anyOf")
            non_null_types = [t for t in any_of if t.get("type") != "null"]
            if len(non_null_types) == 1:
                d["type"] = non_null_types[0].get("type")
                d["nullable"] = True
                for k, v in non_null_types[0].items():
                    if k != "type":
                        d[k] = v
            else:
                d["type"] = "string"
                d["nullable"] = True
                
        for val in d.values():
            if isinstance(val, dict):
                clean_dict(val)
            elif isinstance(val, list):
                for item in val:
                    clean_dict(item)
                    
    clean_dict(schema)
    return schema

class ParsedDebt(BaseModel):
    intent: str = Field(..., description="One of: add_debt, pay_debt, mark_paid, cancel_debt, list_debt, debt_summary, unknown")
    direction: str | None = Field(..., description="'receivable' if the person owes the user, 'payable' if the user owes the person, or null if not applicable")
    person_name: str | None = Field(..., description="Name of the other person involved, or null if not identifiable")
    amount: int | None = Field(..., description="Debt or payment amount as an integer in IDR, or null if not applicable")
    description: str | None = Field(..., description="Short description/reason for the debt, or null if not stated")
    due_date: str | None = Field(..., description="ISO 8601 date (YYYY-MM-DD) if a due date is mentioned, else null. Always left null by the LLM; resolved deterministically in Python.")
    reason: str | None = Field(..., description="Explanation for why intent is 'unknown'; null for all other intents")

    @field_validator("intent", mode="before")
    @classmethod
    def validate_intent(cls, v: str) -> str:
        if not isinstance(v, str):
            return "unknown"
        v = v.strip().lower()
        return v if v in VALID_DEBT_INTENTS else "unknown"

    @field_validator("direction", mode="before")
    @classmethod
    def validate_direction(cls, v):
        if not isinstance(v, str):
            return None
        v = v.strip().lower()
        return v if v in VALID_DEBT_DIRECTIONS else None

VALID_NETWORTH_INTENTS = {
    "add_asset", "update_asset", "delete_asset",
    "add_liability", "update_liability", "delete_liability",
    "show_networth", "unknown"
}

class ParsedNetWorth(BaseModel):
    intent: str = Field(..., description="One of: add_asset, update_asset, delete_asset, add_liability, update_liability, delete_liability, show_networth, unknown")
    type: str | None = Field(..., description="Type of asset (bank, cash, investment, property, other) or liability (loan, credit_card, debt, other), or null if not applicable")
    name: str | None = Field(..., description="Name of the asset or liability, or null if not applicable")
    amount: int | None = Field(..., description="Amount as an integer in IDR, or null if not applicable")
    notes: str | None = Field(..., description="Additional notes or descriptions, or null if not applicable")
    reason: str | None = Field(..., description="Explanation for why intent is 'unknown'; null for all other intents")

    @field_validator("intent", mode="before")
    @classmethod
    def validate_intent(cls, v: str) -> str:
        if not isinstance(v, str):
            return "unknown"
        v = v.strip().lower()
        return v if v in VALID_NETWORTH_INTENTS else "unknown"

# Pydantic schema for analysis validation
class AnalyzeResponse(BaseModel):
    summary: str = Field(..., description="Concise paragraph summary of the user financial health and spending patterns in casual Indonesian")
    insights: list[str] = Field(..., description="List of actionable insights and observations in casual Indonesian")
    anomalies: list[str] = Field(..., description="Detected spending anomalies (unusually high expenses) in casual Indonesian")
    wasteful_spending: list[str] = Field(..., description="Detected wasteful spending (frequent small or unnecessary expenses) in casual Indonesian")
    highest_spending_day: str = Field(..., description="The day with highest spending, formatted nicely (e.g. 'Senin, 15 Jun 2026 sebesar Rp 500.000')")
    trends: list[str] = Field(..., description="Category trend increases or decreases compared to previous transactions in casual Indonesian")
    saving_recommendations: list[str] = Field(..., description="Actionable saving recommendations in casual Indonesian")
    financial_score: int = Field(..., description="Financial score from 0 to 100 based on their savings rate, budgeting, and spending habits")

def normalize_indonesian_currency(text: str, support_k: bool = False) -> str:
    """
    Normalizes Indonesian slang currency formats:
    - rb / ribu -> 1,000
    - jt / juta -> 1,000,000
    - k -> 1,000 (only when support_k=True; opt-in to avoid false positives
      like "video 4k" or "TV 4k" in general transaction/receipt text)
    Handles decimal commas (e.g. 2,5jt -> 2.5jt -> 2500000)
    """
    if not text:
        return ""

    # 1. Replace decimal commas with dots if followed by currency multiplier words
    # e.g., 2,5jt -> 2.5jt
    multiplier_words = r'rb|ribu|jt|juta' + (r'|k' if support_k else '')
    normalized = re.sub(
        rf'(\d+),(\d+)(\s*(?:{multiplier_words})\b)',
        r'\1.\2\3',
        text,
        flags=re.IGNORECASE
    )

    # 2. Process 'jt' and 'juta' (multiply by 1,000,000)
    def replace_jt(match):
        val = float(match.group(1))
        return str(int(val * 1_000_000))

    normalized = re.sub(
        r'(\d+(?:\.\d+)?)\s*(?:jt|juta)\b',
        replace_jt,
        normalized,
        flags=re.IGNORECASE
    )

    # 3. Process 'rb' and 'ribu' (multiply by 1,000)
    def replace_rb(match):
        val = float(match.group(1))
        return str(int(val * 1_000))

    normalized = re.sub(
        r'(\d+(?:\.\d+)?)\s*(?:rb|ribu)\b',
        replace_rb,
        normalized,
        flags=re.IGNORECASE
    )

    # 4. Process 'k' suffix (multiply by 1,000) — opt-in only
    if support_k:
        def replace_k(match):
            val = float(match.group(1))
            return str(int(val * 1_000))

        normalized = re.sub(
            r'(\d+(?:\.\d+)?)\s*k\b',
            replace_k,
            normalized,
            flags=re.IGNORECASE
        )

    return normalized

def resolve_due_date(text: str, reference_date: datetime | None = None) -> str | None:
    """
    Deterministically resolves relative Indonesian due-date phrases found in the
    ORIGINAL (non-amount-normalized) input text to an ISO 8601 date string
    (YYYY-MM-DD), using Asia/Jakarta as the reference timezone for "today".

    Supported phrases:
    - "besok"        -> tomorrow (+1 day)
    - "lusa"         -> day after tomorrow (+2 days)
    - "minggu depan" -> +7 days
    - "tanggal N"    -> nearest occurrence of day-of-month N; if N has already
                        passed this month (or is today), rolls to next month.
                        Clamped to the target month's actual length.

    Returns None if no recognized date phrase is found.
    """
    if not text:
        return None

    now = reference_date if reference_date is not None else datetime.now(JAKARTA_TZ)
    lower_text = text.lower()

    if "lusa" in lower_text:
        target = now + timedelta(days=2)
        return target.strftime("%Y-%m-%d")

    if "besok" in lower_text:
        target = now + timedelta(days=1)
        return target.strftime("%Y-%m-%d")

    if "minggu depan" in lower_text:
        target = now + timedelta(days=7)
        return target.strftime("%Y-%m-%d")

    match = re.search(r'\btanggal\s+(\d{1,2})\b', lower_text)
    if match:
        day = int(match.group(1))
        if 1 <= day <= 31:
            year, month = now.year, now.month
            _, days_in_month = calendar.monthrange(year, month)
            clamped_day = min(day, days_in_month)
            candidate = now.replace(day=clamped_day, hour=0, minute=0, second=0, microsecond=0)
            if candidate.date() <= now.date():
                # Already passed (or is today) this month -> roll to next month
                if month == 12:
                    year, month = year + 1, 1
                else:
                    month += 1
                _, days_in_month = calendar.monthrange(year, month)
                clamped_day = min(day, days_in_month)
                candidate = candidate.replace(year=year, month=month, day=clamped_day)
            return candidate.strftime("%Y-%m-%d")

    return None

def fallback_parse_networth(text: str) -> dict:
    """
    Simple rule-based fallback for net worth parsing when no LLM is configured.
    """
    normalized = normalize_indonesian_currency(text, support_k=True)
    lower_text = text.lower()
    
    amounts = re.findall(r'\d+', normalized)
    amount = int(amounts[0]) if amounts else None

    # Try to find a capitalized word for the name (e.g. BCA, Dompet, Saham)
    candidates = re.findall(r'\b([A-Z][a-zA-Z0-9_]{1,})\b', text)
    name = None
    if candidates:
        name = candidates[0]
    else:
        for w in ["dompet", "cash", "saham", "reksa dana", "crypto", "cicilan motor", "motor", "mobil", "rumah"]:
            if w in lower_text:
                name = w.title()
                break

    # Determine intent
    if any(w in lower_text for w in ["show_networth", "networth", "kekayaan", "net worth"]):
        return {
            "intent": "show_networth",
            "type": None,
            "name": None,
            "amount": None,
            "notes": None,
            "reason": None
        }

    is_delete = any(w in lower_text for w in ["hapus", "delete", "buang"])
    is_update = any(w in lower_text for w in ["update", "ubah", "set", "ganti", "jadi"])
    is_liability = any(w in lower_text for w in ["cicilan", "hutang", "utang", "pinjaman", "kewajiban", "liability", "liabilities", "credit card", "kartu kredit"])

    intent = "unknown"
    if is_delete:
        intent = "delete_liability" if is_liability else "delete_asset"
    elif is_update:
        intent = "update_liability" if is_liability else "update_asset"
    else:
        if any(w in lower_text for w in ["saldo", "tabungan", "cash", "saham", "investasi", "aset", "asset"]):
            intent = "add_asset"
        elif is_liability:
            intent = "add_liability"
        else:
            intent = "add_asset"  # fallback default

    # Determine type
    item_type = "other"
    if is_liability:
        if any(w in lower_text for w in ["kartu kredit", "cc"]):
            item_type = "credit_card"
        elif any(w in lower_text for w in ["cicilan", "pinjaman", "loan", "kpr"]):
            item_type = "loan"
        else:
            item_type = "debt"
    else:
        if any(w in lower_text for w in ["tabungan", "bank", "bca", "mandiri", "bni", "bri"]):
            item_type = "bank"
        elif any(w in lower_text for w in ["cash", "dompet"]):
            item_type = "cash"
        elif any(w in lower_text for w in ["saham", "reksa dana", "investasi", "crypto"]):
            item_type = "investment"

    # Validate required parameters
    if intent in ["delete_asset", "delete_liability", "update_asset", "update_liability", "add_asset", "add_liability"]:
        if not name:
            return {
                "intent": "unknown",
                "type": None,
                "name": None,
                "amount": None,
                "notes": None,
                "reason": "Tidak dapat mengenali nama aset atau kewajiban dari teks."
            }
        if intent in ["add_asset", "add_liability", "update_asset", "update_liability"] and amount is None:
            return {
                "intent": "unknown",
                "type": None,
                "name": name,
                "amount": None,
                "notes": None,
                "reason": "Tidak dapat mendeteksi jumlah uang dari teks."
            }

    return {
        "intent": intent,
        "type": item_type if intent not in ["delete_asset", "delete_liability"] else None,
        "name": name,
        "amount": amount,
        "notes": None,
        "reason": None
    }

def fallback_parse_debt(text: str) -> dict:
    """
    Simple rule-based fallback for debt intent parsing, used only when no LLM
    is configured. Covers the canonical phrasing patterns; anything else is
    returned as intent="unknown" with a reason.
    """
    normalized = normalize_indonesian_currency(text, support_k=True)
    lower_text = text.lower()

    amounts = re.findall(r'\d+', normalized)
    amount = int(amounts[0]) if amounts else None

    # Naive person-name guess: first capitalized word that isn't "Saya"
    candidates = re.findall(r'\b([A-Z][a-zA-Z]{1,})\b', text)
    person_name = next((c for c in candidates if c.lower() != "saya"), None)

    def make_unknown(reason: str) -> dict:
        return {
            "intent": "unknown",
            "direction": None,
            "person_name": person_name,
            "amount": amount,
            "description": None,
            "due_date": None,
            "reason": reason,
        }

    if not person_name:
        return make_unknown("Tidak dapat mengenali nama orang dalam teks.")

    says_saya_owes = bool(re.search(r'\bsaya\b.{0,15}\b(hutang|berhutang|utang)\b', lower_text))

    # mark_paid: "lunas"
    if "lunas" in lower_text:
        direction = "payable" if says_saya_owes else "receivable"
        return {
            "intent": "mark_paid",
            "direction": direction,
            "person_name": person_name,
            "amount": None,
            "description": None,
            "due_date": None,
            "reason": None,
        }

    # cancel_debt: "hapus" or "batal"
    # No reliable "saya" signal distinguishes direction for cancel phrasing in
    # this naive fallback, so default to "payable" (matches canonical usage
    # like "hapus hutang Budi" = cancelling the user's own debt to Budi).
    if any(w in lower_text for w in ["hapus", "batal"]):
        direction = "payable"
        return {
            "intent": "cancel_debt",
            "direction": direction,
            "person_name": person_name,
            "amount": None,
            "description": None,
            "due_date": None,
            "reason": None,
        }

    # pay_debt: "bayar" (payment against existing debt)
    if "bayar" in lower_text:
        says_saya_paid = bool(re.search(r'\bsaya\b.{0,20}\bbayar\b', lower_text))
        direction = "payable" if says_saya_paid else "receivable"
        if amount is None:
            return make_unknown("Tidak dapat menemukan jumlah pembayaran.")
        return {
            "intent": "pay_debt",
            "direction": direction,
            "person_name": person_name,
            "amount": amount,
            "description": None,
            "due_date": None,
            "reason": None,
        }

    # add_debt: "hutang"/"utang"/"berhutang"
    if any(w in lower_text for w in ["hutang", "utang", "berhutang"]):
        if amount is None:
            return make_unknown("Tidak dapat menemukan jumlah hutang.")
        direction = "payable" if says_saya_owes else "receivable"
        return {
            "intent": "add_debt",
            "direction": direction,
            "person_name": person_name,
            "amount": amount,
            "description": None,
            "due_date": None,
            "reason": None,
        }

    return make_unknown("Tidak dapat mengenali maksud (intent) dari teks.")

def fallback_parse(text: str) -> dict:
    """
    Rule-based fallback parser used when Gemini API is unavailable.
    """
    normalized = normalize_indonesian_currency(text)
    
    # Extract first sequence of digits for the amount
    amounts = re.findall(r'\d+', normalized)
    amount = int(amounts[0]) if amounts else 0
    
    lower_text = text.lower()
    
    # Simple logic for expense vs income
    is_income = any(w in lower_text for w in ["gaji", "income", "masuk", "transfer", "salary", "refund"])
    tx_type = "income" if is_income else "expense"
    
    # Simple logic for category classification
    category = "other"
    if any(w in lower_text for w in ["makan", "minum", "bakso", "kopi", "warteg", "food", "dining", "cafe", "restoran", "starbucks", "kopitiam"]):
        category = "food"
    elif any(w in lower_text for w in ["indomaret", "alfamart", "supermarket", "belanja bulanan", "grocery", "groceries", "minimarket"]):
        category = "groceries"
    elif any(w in lower_text for w in ["tokopedia", "shopee", "lazada", "bukalapak", "mall", "belanja online", "marketplace"]):
        category = "shopping"
    elif any(w in lower_text for w in ["ojek", "uber", "gojek", "grab", "bensin", "transport", "mrt", "bus", "kereta", "shell", "pertamina"]):
        category = "transport"
    elif any(w in lower_text for w in ["listrik", "air", "wifi", "internet", "pulsa", "bill", "utilities"]):
        category = "utilities"
    elif any(w in lower_text for w in ["nonton", "bioskop", "netflix", "game", "entertainment", "hiburan", "liburan"]):
        category = "entertainment"
    elif is_income:
        category = "salary"

    # Clean description (remove the parsed amount terms)
    description = re.sub(r'\b\d+(?:\.\d+)?\s*(?:rb|ribu|jt|juta)?\b', '', text, flags=re.IGNORECASE)
    description = re.sub(r'\s+', ' ', description).strip()
    if not description:
        description = text
        
    return {
        "type": tx_type,
        "category": category,
        "amount": amount,
        "description": description
    }

def fallback_analyze(transactions: list) -> dict:
    """
    Rule-based fallback analyzer in friendly conversational Indonesian.
    Calculates anomalies, wasteful spending, highest-spending day, trends, recommendations, and financial score.
    """
    if not transactions:
        return {
            "summary": "Belum ada catatan riwayat transaksi nih bro buat dianalisis.",
            "insights": ["Yuk langsung catat aja pengeluaran/pemasukan lu biar nanti gua kasih analisis gokil!"],
            "anomalies": [],
            "wasteful_spending": [],
            "highest_spending_day": "Belum ada data",
            "trends": [],
            "saving_recommendations": ["Sisihin minimal 10% pendapatan pas gajian."],
            "financial_score": 50
        }
    
    total_income = sum(tx.get("amount", 0) for tx in transactions if tx.get("type") == "income")
    total_expense = sum(tx.get("amount", 0) for tx in transactions if tx.get("type") == "expense")
    
    # 1. Highest Spending Day
    daily_spending = {}
    for tx in transactions:
        if tx.get("type") == "expense":
            dt_str = tx.get("transaction_date") or tx.get("created_at") or ""
            if dt_str:
                date_key = dt_str[:10]
                daily_spending[date_key] = daily_spending.get(date_key, 0) + tx.get("amount", 0)
    
    highest_day = "Belum ada data pengeluaran"
    if daily_spending:
        max_date = max(daily_spending, key=daily_spending.get)
        max_amount = daily_spending[max_date]
        try:
            dt = datetime.fromisoformat(max_date.replace('Z', '+00:00'))
            formatted_date = dt.strftime('%A, %d %b %Y')
            days_id = {'Monday': 'Senin', 'Tuesday': 'Selasa', 'Wednesday': 'Rabu', 'Thursday': 'Kamis', 'Friday': 'Jumat', 'Saturday': 'Sabtu', 'Sunday': 'Minggu'}
            months_id = {'Jan': 'Jan', 'Feb': 'Feb', 'Mar': 'Mar', 'Apr': 'Apr', 'May': 'Mei', 'Jun': 'Jun', 'Jul': 'Jul', 'Aug': 'Agu', 'Sep': 'Sep', 'Oct': 'Okt', 'Nov': 'Nov', 'Dec': 'Des'}
            for en, idr in days_id.items():
                formatted_date = formatted_date.replace(en, idr)
            for en, idr in months_id.items():
                formatted_date = formatted_date.replace(en, idr)
            highest_day = f"{formatted_date} sebesar Rp {max_amount:,}"
        except Exception:
            highest_day = f"{max_date} sebesar Rp {max_amount:,}"

    # 2. Spend Anomalies (expenses > 500,000 or > 2x average expense)
    expenses = [tx for tx in transactions if tx.get("type") == "expense"]
    avg_expense = sum(tx.get("amount", 0) for tx in expenses) / len(expenses) if expenses else 0
    anomalies = []
    for tx in expenses:
        amt = tx.get("amount", 0)
        if amt > 500000 or (avg_expense > 0 and amt > 2.5 * avg_expense):
            anomalies.append(f"Pembelian '{tx.get('description')}' senilai Rp {amt:,} ini lumayan gede dibanding rata-rata belanja lu bro.")

    # 3. Wasteful Spending (Frequent small expenses under food/entertainment or description pattern)
    desc_counts = {}
    for tx in expenses:
        desc = tx.get("description", "").lower()
        if desc:
            desc_counts[desc] = desc_counts.get(desc, 0) + 1
            
    wasteful = []
    for desc, count in desc_counts.items():
        if count >= 3:
            item_total = sum(tx.get("amount", 0) for tx in expenses if tx.get("description", "").lower() == desc)
            wasteful.append(f"Belanja '{desc}' berulang sampai {count} kali (total Rp {item_total:,}). Hati-hati bocor alus nih!")

    # 4. Trends
    trends = []
    cat_expenses = {}
    for tx in expenses:
        cat = tx.get("category", "other")
        cat_expenses[cat] = cat_expenses.get(cat, 0) + tx.get("amount", 0)
        
    biggest_category = "None"
    biggest_amount = 0
    if cat_expenses:
        biggest_category = max(cat_expenses, key=cat_expenses.get)
        biggest_amount = cat_expenses[biggest_category]
        trends.append(f"Kategori '{biggest_category}' lagi mendominasi pengeluaran lu bulan ini (Rp {biggest_amount:,}).")

    # 5. Financial Score and saving rate recommendations
    savings_rate = 0
    financial_score = 50
    if total_income > 0:
        savings = total_income - total_expense
        savings_rate = (savings / total_income) * 100
        financial_score = int(50 + (savings_rate / 2))
        financial_score = max(0, min(100, financial_score))
    elif total_expense > 0:
        financial_score = max(10, 50 - int(total_expense / 100000))
        financial_score = max(5, min(95, financial_score))

    recs = []
    if savings_rate < 10:
        recs.append("Coba batasi belanja tersier (jajan, kopi, belanja online) maksimal 20% dari pemasukan.")
        recs.append("Tabung/investasikan dana minimal 10% di awal bulan sebelum dipakai belanja.")
    else:
        recs.append("Tabungan lu udah oke! Coba alokasikan sebagian ke reksa dana atau investasi lainnya.")
        recs.append("Buat dana darurat setara 3-6 kali pengeluaran bulanan lu.")

    summary = (
        f"Nih ringkasan dana lu bro: total pemasukan lu ada Rp {total_income:,}, terus pengeluaran lu totalnya Rp {total_expense:,}. "
        f"Kondisi keuangan lu dapet skor {financial_score}/100."
    )

    insights = [
        f"Pola belanja lu nih: Area jajan paling gede ada di '{biggest_category}' (Rp {biggest_amount:,})."
    ]
    if total_expense > total_income:
        insights.append("Duh bahaya nih! Pengeluaran lu lebih gede dari pemasukan bulan ini.")

    return {
        "summary": summary,
        "insights": insights,
        "anomalies": anomalies,
        "wasteful_spending": wasteful,
        "highest_spending_day": highest_day,
        "trends": trends,
        "saving_recommendations": recs,
        "financial_score": financial_score
    }

class ParserService:
    def __init__(self):
        self.api_key = os.getenv("GEMINI_API_KEY")
        self.model_name = os.getenv("GEMINI_MODEL", "gemini-1.5-flash")
        
        self.llm_base_url = os.getenv("LLM_BASE_URL")
        self.llm_model = os.getenv("LLM_MODEL")
        self.llm_api_key = os.getenv("LLM_API_KEY")
        
        self.is_gemini_configured = False
        self.is_custom_llm_configured = False
        
        if self.api_key and self.api_key != "YOUR_GEMINI_API_KEY_HERE":
            try:
                genai.configure(api_key=self.api_key)
                self.is_gemini_configured = True
                logger.info("Gemini API initialized successfully.")
            except Exception as e:
                logger.error(f"Failed to configure Gemini API: {e}")
                
        if self.llm_base_url:
            self.is_custom_llm_configured = True
            logger.info(f"Custom LLM initialized successfully with base URL: {self.llm_base_url}")
            
        self.is_configured = self.is_gemini_configured or self.is_custom_llm_configured
        if not self.is_configured:
            logger.warning("No LLM configurations found. Falling back to rule-based parser/analyzer.")

    def _call_custom_llm(self, prompt: str, system_prompt: str = None) -> str:
        url = f"{self.llm_base_url.rstrip('/')}/chat/completions"
        
        messages = []
        if system_prompt:
            messages.append({"role": "system", "content": system_prompt})
        messages.append({"role": "user", "content": prompt})
        
        payload = {
            "model": self.llm_model or "gpt-4o",
            "messages": messages
        }
        
        # Standard OpenAI json format
        payload["response_format"] = {"type": "json_object"}
        
        data = json.dumps(payload).encode("utf-8")
        
        req = urllib.request.Request(url, data=data, method="POST")
        req.add_header("Content-Type", "application/json")
        if self.llm_api_key:
            req.add_header("Authorization", f"Bearer {self.llm_api_key}")
            
        try:
            with urllib.request.urlopen(req, timeout=15) as response:
                resp_data = json.loads(response.read().decode("utf-8"))
                return resp_data["choices"][0]["message"]["content"]
        except Exception as e:
            logger.error(f"Custom LLM HTTP request failed: {e}")
            raise e

    def parse_transaction(self, text: str) -> ParsedTransaction:
        preprocessed_text = normalize_indonesian_currency(text)
        logger.info(f"Original text: '{text}' | Preprocessed text: '{preprocessed_text}'")

        if not self.is_configured:
            logger.info("Using local fallback rule-based parser.")
            parsed_data = fallback_parse(text)
            return ParsedTransaction(**parsed_data)

        if self.is_custom_llm_configured:
            try:
                system_prompt = "You are a personal finance assistant transaction parser."
                prompt = f"""
Parse the following text and extract transaction details.
The input text has been normalized to help you: "{preprocessed_text}"

Return a JSON object containing:
1. "type": "expense" or "income".
2. "category": a normalized category name (lowercase, one of: "food", "groceries", "shopping", "transport", "utilities", "entertainment", "salary", "other").
3. "amount": the transaction amount as an integer.
4. "description": what the transaction was for (exclude the amount or currency symbols, and clean up into friendly informal Indonesian if necessary).

Return ONLY a JSON object. Do not wrap in markdown tags like ```json.
"""
                raw_json = self._call_custom_llm(prompt, system_prompt).strip()
                if raw_json.startswith("```"):
                    lines = raw_json.split("\n")
                    if lines[0].startswith("```"):
                        lines = lines[1:]
                    if lines[-1].startswith("```"):
                        lines = lines[:-1]
                    raw_json = "\n".join(lines).strip()
                    
                logger.info(f"Custom LLM raw response: {raw_json}")
                parsed_data = json.loads(raw_json)
                return ParsedTransaction(**parsed_data)
            except Exception as e:
                logger.error(f"Custom LLM parse failed: {e}. Falling back to rule-based parser.")
                parsed_data = fallback_parse(text)
                return ParsedTransaction(**parsed_data)

        try:
            prompt = f"""
You are a personal finance assistant transaction parser. Parse the following text and extract transaction details.
The input text has been normalized to help you: "{preprocessed_text}"

Return a JSON object containing:
1. "type": "expense" or "income".
2. "category": a normalized category name (lowercase, one of: "food", "groceries", "shopping", "transport", "utilities", "entertainment", "salary", "other").
3. "amount": the transaction amount as an integer.
4. "description": what the transaction was for (exclude the amount or currency symbols, and clean up into friendly informal Indonesian if necessary).

Return ONLY a JSON object. Do not wrap in markdown tags like ```json.
"""
            model = genai.GenerativeModel(self.model_name)
            response = model.generate_content(
                prompt,
                generation_config=genai.types.GenerationConfig(
                    response_mime_type="application/json",
                    response_schema=clean_gemini_schema(ParsedTransaction),
                )
            )
            
            raw_json = response.text.strip()
            logger.info(f"Gemini raw response: {raw_json}")
            
            parsed_data = json.loads(raw_json)
            return ParsedTransaction(**parsed_data)
            
        except Exception as e:
            logger.error(f"Gemini API parse failed: {e}. Falling back to rule-based parser.")
            parsed_data = fallback_parse(text)
            return ParsedTransaction(**parsed_data)

    def parse_debt(self, text: str) -> ParsedDebt:
        preprocessed_text = normalize_indonesian_currency(text, support_k=True)
        logger.info(f"Parsing debt text: '{text}' | Preprocessed: '{preprocessed_text}'")

        if not self.is_configured:
            logger.info("Using local fallback rule-based debt parser.")
            parsed_data = fallback_parse_debt(text)
            result = ParsedDebt(**parsed_data)
            result.due_date = resolve_due_date(text)
            return result

        prompt = f"""
You are a personal debt-tracking assistant parser for an Indonesian personal finance app.
Parse the following text and extract debt-related intent and details.

The input text has been normalized for currency amounts to help you: "{preprocessed_text}"

Supported "intent" values (choose exactly one):
- "add_debt": a new debt is being recorded (someone owes someone money).
- "pay_debt": a payment is being made against an existing debt.
- "mark_paid": an existing debt is being marked as fully settled/paid off (e.g. "lunas").
- "cancel_debt": an existing debt record should be deleted/cancelled (e.g. "hapus", "batal").
- "list_debt": the user wants to see a list of their debts.
- "debt_summary": the user wants a summary/total of their debts.
- "unknown": you cannot confidently determine the intent, person, or amount.

"direction" must be one of "receivable", "payable", or null:
- "receivable": the OTHER PERSON owes THE USER money (money is coming TO the user).
- "payable": THE USER owes the OTHER PERSON money (money is going FROM the user).
- null: not applicable (e.g. for "list_debt", "debt_summary", or "unknown").

CRITICAL direction-inference rule for Indonesian phrasing (this is inherently ambiguous and
you must follow this rule precisely, since it cannot be reliably determined by simple keyword
matching):
- If the sentence structure is "<Person> hutang/utang ke saya ..." OR simply
  "<Person> hutang/utang ..." (without "saya" as the one who owes) -> direction is "receivable"
  (the named person owes the user).
- If the sentence structure is "saya hutang/berhutang <Person> ..." (the user explicitly states
  THEY are the one who owes) -> direction is "payable" (the user owes the named person).
- The same subject-identification rule applies to "bayar" (payment) and "lunas" (paid off) and
  "hapus"/"batal" (cancel) sentences: identify WHO is the subject performing/owing the action
  relative to "saya" (the user) to determine direction.

Extract:
1. "intent": one of the 7 values above.
2. "direction": "receivable", "payable", or null, following the rule above.
3. "person_name": the name of the other person involved (proper noun, as written), or null if
   not identifiable.
4. "amount": the amount as an integer (IDR), or null if not applicable to this intent
   (e.g. null for mark_paid, cancel_debt, list_debt, debt_summary).
5. "description": a short description of what the debt/payment was for, in clean informal
   Indonesian, or null if not stated.
6. "due_date": ALWAYS return null for this field. Due date resolution is handled separately
   by deterministic code, not by you. Do not attempt to compute or guess a date.
7. "reason": if and only if intent is "unknown", a brief explanation in Indonesian of why
   (e.g. "Tidak dapat mengenali nama orang dalam teks."). Otherwise null.

Canonical examples (follow these exactly for phrasing patterns):

Example 1:
Input: "Andi hutang ke saya 200rb"
Output: {{"intent": "add_debt", "direction": "receivable", "person_name": "Andi", "amount": 200000, "description": null, "due_date": null, "reason": null}}

Example 2:
Input: "saya hutang Budi 500rb"
Output: {{"intent": "add_debt", "direction": "payable", "person_name": "Budi", "amount": 500000, "description": null, "due_date": null, "reason": null}}

Example 3:
Input: "Andi bayar 100rb"
Output: {{"intent": "pay_debt", "direction": "receivable", "person_name": "Andi", "amount": 100000, "description": null, "due_date": null, "reason": null}}

Example 4:
Input: "saya sudah bayar Budi 250rb"
Output: {{"intent": "pay_debt", "direction": "payable", "person_name": "Budi", "amount": 250000, "description": null, "due_date": null, "reason": null}}

Example 5:
Input: "utang Andi sudah lunas"
Output: {{"intent": "mark_paid", "direction": "receivable", "person_name": "Andi", "amount": null, "description": null, "due_date": null, "reason": null}}

Example 6:
Input: "hapus hutang Budi"
Output: {{"intent": "cancel_debt", "direction": "payable", "person_name": "Budi", "amount": null, "description": null, "due_date": null, "reason": null}}

Rules:
- All monetary values MUST be plain integers in IDR (no "Rp", no dots, no commas).
- If you cannot confidently identify a person name AND (where applicable) an amount, return
  intent "unknown" with a populated "reason" rather than guessing.
- Return ONLY the raw JSON object. Do not wrap in markdown tags like ```json.
"""

        if self.is_custom_llm_configured:
            try:
                system_prompt = "You are a personal debt-tracking assistant parser."
                raw_json = self._call_custom_llm(prompt, system_prompt).strip()
                if raw_json.startswith("```"):
                    lines = raw_json.split("\n")
                    if lines[0].startswith("```"):
                        lines = lines[1:]
                    if lines[-1].startswith("```"):
                        lines = lines[:-1]
                    raw_json = "\n".join(lines).strip()

                logger.info(f"Custom LLM debt raw response: {raw_json}")
                parsed_data = json.loads(raw_json)
                result = ParsedDebt(**parsed_data)
                result.due_date = resolve_due_date(text)
                return result
            except Exception as e:
                logger.error(f"Custom LLM debt parse failed: {e}. Falling back to rule-based parser.")
                parsed_data = fallback_parse_debt(text)
                result = ParsedDebt(**parsed_data)
                result.due_date = resolve_due_date(text)
                return result

        try:
            model = genai.GenerativeModel(self.model_name)
            response = model.generate_content(
                prompt,
                generation_config=genai.types.GenerationConfig(
                    response_mime_type="application/json",
                    response_schema=clean_gemini_schema(ParsedDebt),
                )
            )

            raw_json = response.text.strip()
            logger.info(f"Gemini debt raw response: {raw_json}")

            parsed_data = json.loads(raw_json)
            result = ParsedDebt(**parsed_data)
            result.due_date = resolve_due_date(text)
            return result

        except Exception as e:
            logger.error(f"Gemini API debt parse failed: {e}. Falling back to rule-based parser.")
            parsed_data = fallback_parse_debt(text)
            result = ParsedDebt(**parsed_data)
            result.due_date = resolve_due_date(text)
            return result

    def parse_networth(self, text: str) -> ParsedNetWorth:
        preprocessed_text = normalize_indonesian_currency(text, support_k=True)
        logger.info(f"Parsing net worth text: '{text}' | Preprocessed: '{preprocessed_text}'")

        if not self.is_configured:
            logger.info("Using local fallback rule-based net worth parser.")
            parsed_data = fallback_parse_networth(text)
            return ParsedNetWorth(**parsed_data)

        prompt = f"""
You are a personal finance assistant parser for an Indonesian personal finance app.
Parse the following text and extract net worth related intent and details (assets and liabilities).

The input text has been normalized for currency amounts to help you: "{preprocessed_text}"

Supported "intent" values (choose exactly one):
- "add_asset": a new asset is being recorded (e.g. bank account, cash, investments, properties).
- "update_asset": updating the value/amount of an existing asset.
- "delete_asset": deleting/removing an asset.
- "add_liability": a new liability/debt/loan is being recorded.
- "update_liability": updating the value/amount of an existing liability.
- "delete_liability": deleting/removing a liability.
- "show_networth": user wants to view their net worth status or history.
- "unknown": you cannot confidently determine the intent, name, or amount.

"type" (optional, null if not applicable):
- For assets, must be one of: "bank" (for bank accounts/savings), "cash" (cash on hand/wallet), "investment" (stocks, mutual funds, gold, crypto), "property" (real estate, vehicles), or "other".
- For liabilities, must be one of: "loan" (mortgages, auto loans, personal loans), "credit_card" (credit card debts), "debt" (money owed to other people), or "other".

"name" (optional, null if not applicable):
- The name of the asset or liability (e.g., "BCA", "Dompet", "Saham", "Cicilan Motor"). Keep it concise.

"amount" (optional, null if not applicable):
- The amount as an integer in IDR.

"notes" (optional, null if not applicable):
- Any additional details/notes provided.

"reason" (optional, only populated when intent is "unknown"):
- Explanation in Indonesian of why the intent is unknown (e.g., "Tidak dapat mendeteksi jumlah uang dari teks.").

Canonical examples (follow these exactly for phrasing patterns):

Example 1:
Input: "saldo BCA saya 12 juta"
Output: {{"intent": "add_asset", "type": "bank", "name": "BCA", "amount": 12000000, "notes": null, "reason": null}}

Example 2:
Input: "cash di dompet 500rb"
Output: {{"intent": "add_asset", "type": "cash", "name": "Dompet", "amount": 500000, "notes": null, "reason": null}}

Example 3:
Input: "saham saya sekarang 5 juta"
Output: {{"intent": "add_asset", "type": "investment", "name": "Saham", "amount": 5000000, "notes": null, "reason": null}}

Example 4:
Input: "cicilan motor sisa 2 juta"
Output: {{"intent": "add_liability", "type": "loan", "name": "Cicilan Motor", "amount": 2000000, "notes": null, "reason": null}}

Example 5:
Input: "update saldo BCA jadi 15 juta"
Output: {{"intent": "update_asset", "type": "bank", "name": "BCA", "amount": 15000000, "notes": null, "reason": null}}

Example 6:
Input: "hapus aset BCA"
Output: {{"intent": "delete_asset", "type": null, "name": "BCA", "amount": null, "notes": null, "reason": null}}

Example 7:
Input: "tampilkan kekayaan bersih saya"
Output: {{"intent": "show_networth", "type": null, "name": null, "amount": null, "notes": null, "reason": null}}

Rules:
- All monetary values MUST be plain integers in IDR (no "Rp", no dots, no commas).
- If you cannot confidently identify the asset/liability name AND (where applicable) the amount, return intent "unknown" with a populated "reason".
- Return ONLY the raw JSON object. Do not wrap in markdown tags like ```json.
"""

        if self.is_custom_llm_configured:
            try:
                system_prompt = "You are a personal finance assistant parser."
                raw_json = self._call_custom_llm(prompt, system_prompt).strip()
                if raw_json.startswith("```"):
                    lines = raw_json.split("\n")
                    if lines[0].startswith("```"):
                        lines = lines[1:]
                    if lines[-1].startswith("```"):
                        lines = lines[:-1]
                    raw_json = "\n".join(lines).strip()

                logger.info(f"Custom LLM networth raw response: {raw_json}")
                parsed_data = json.loads(raw_json)
                return ParsedNetWorth(**parsed_data)
            except Exception as e:
                logger.error(f"Custom LLM networth parse failed: {e}. Falling back to rule-based parser.")
                parsed_data = fallback_parse_networth(text)
                return ParsedNetWorth(**parsed_data)

        try:
            model = genai.GenerativeModel(self.model_name)
            response = model.generate_content(
                prompt,
                generation_config=genai.types.GenerationConfig(
                    response_mime_type="application/json",
                    response_schema=clean_gemini_schema(ParsedNetWorth),
                )
            )

            raw_json = response.text.strip()
            logger.info(f"Gemini networth raw response: {raw_json}")

            parsed_data = json.loads(raw_json)
            return ParsedNetWorth(**parsed_data)

        except Exception as e:
            logger.error(f"Gemini API networth parse failed: {e}. Falling back to rule-based parser.")
            parsed_data = fallback_parse_networth(text)
            return ParsedNetWorth(**parsed_data)

    def parse_receipt(self, text: str) -> ParsedReceipt:
        logger.info(f"Parsing receipt text ({len(text)} chars)")

        if not self.is_configured:
            raise ValueError(
                "Receipt extraction requires a configured LLM but none is available."
            )

        receipt_prompt = f"""
You are a receipt parser. Extract structured data from the following OCR text of a receipt.

OCR Text:
\"\"\"{text}\"\"\"

Return a JSON object with:
1. "merchant": the store/restaurant name (string, empty if not found).
2. "items": a list of objects, each with "name" (string), "qty" (integer, default 1), "price" (integer, unit price in IDR, no "Rp" or dots).
3. "total": the total amount as an integer in IDR (no "Rp", no dots, no commas).
4. "date": the date on the receipt in ISO 8601 format (e.g. "2026-07-01"), or null if not found.
5. "category": infer the most likely spending category from the merchant name and items. Must be one of:
   - "food" — restaurants, cafes, warung, makanan, minuman (e.g. Starbucks, warteg, restoran, cafe)
   - "groceries" — minimarket, supermarket, grocery store (e.g. Indomaret, Alfamart, supermarket, belanja bulanan)
   - "shopping" — online marketplace, malls, clothing, electronics (e.g. Tokopedia, Shopee, Lazada, mall)
   - "transport" — fuel, ride-hailing, public transport (e.g. Shell, Pertamina, Gojek, Grab)
   - "utilities" — electricity, water, internet, phone bills
   - "entertainment" — movies, streaming, games, travel
   - "other" — cannot determine

   Use BOTH merchant name AND item names together. If merchant alone is enough (e.g. "Shell"), prefer it. Fall back to item patterns if merchant is unclear.

Rules:
- All monetary values MUST be plain integers in IDR (e.g. 25000, not "Rp 25.000" or "25.000").
- If a quantity is not explicitly printed, default "qty" to 1.
- If you cannot determine a field, use its default (empty string for merchant, empty list for items, 0 for total, null for date).
- Do NOT hallucinate data that is not present in the OCR text.
- Return ONLY the raw JSON object. Do not wrap in markdown tags like ```json.
"""
        system_prompt = "You are a receipt data extraction assistant."

        try:
            if self.is_custom_llm_configured:
                raw_json = self._call_custom_llm(receipt_prompt, system_prompt).strip()
                if raw_json.startswith("```"):
                    lines = raw_json.split("\n")
                    if lines[0].startswith("```"):
                        lines = lines[1:]
                    if lines[-1].startswith("```"):
                        lines = lines[:-1]
                    raw_json = "\n".join(lines).strip()
            else:
                model = genai.GenerativeModel(self.model_name)
                response = model.generate_content(
                    receipt_prompt,
                    generation_config=genai.types.GenerationConfig(
                        response_mime_type="application/json",
                        response_schema=clean_gemini_schema(ParsedReceipt),
                    )
                )
                raw_json = response.text.strip()

            logger.info(f"Receipt LLM raw response: {raw_json}")
            receipt_data = json.loads(raw_json)
            return ParsedReceipt(**receipt_data)

        except ValueError:
            raise
        except Exception as e:
            logger.error(f"Receipt LLM parse failed: {e}")
            raise ValueError(f"Failed to extract receipt data: {e}") from e

    def analyze_transactions(self, transactions: list) -> AnalyzeResponse:
        logger.info(f"Analyzing {len(transactions)} transactions")

        if not self.is_configured:
            logger.info("Using local fallback rule-based analyzer.")
            analysis_data = fallback_analyze(transactions)
            return AnalyzeResponse(**analysis_data)

        if self.is_custom_llm_configured:
            try:
                tx_lines = []
                for tx in transactions:
                    date_val = tx.get("transaction_date") or tx.get("created_at") or "unknown"
                    tx_lines.append(
                        f"- [{date_val}] {tx.get('type')} | {tx.get('category')} | Amount: {tx.get('amount')} | Description: {tx.get('description')}"
                    )
                txs_formatted = "\n".join(tx_lines)

                system_prompt = "You are a expert personal finance advisor."
                prompt = f"""
Analyze the following list of user transactions and generate spending analysis metrics:
1. summary: A friendly paragraph summarizing their financial health and spending patterns in casual Indonesian.
2. insights: List of observations and tips.
3. anomalies: List of unusually high or sudden spikes in spending.
4. wasteful_spending: List of frequent small or unnecessary spending items (like ordering coffee/camilan too often).
5. highest_spending_day: The single day with the highest total expenses, formatted nicely (e.g. "Senin, 15 Jun 2026 sebesar Rp 500.000").
6. trends: Any category spending trend increases or decreases.
7. saving_recommendations: Practical saving recommendations.
8. financial_score: An integer from 0 to 100 based on their savings rate, budgeting, and habits.

**Crucial Constraint**: Write all output text values (summary, insights, anomalies, wasteful_spending, trends, saving_recommendations) in informal/casual Indonesian (Bahasa Indonesia gaul/santai, using terms like "lu", "gua", "nih", "bro", "sist", "lho", "coba deh", "yuk", "boncos", "hemat"). Speak like a close supportive friend advising them on their money. Keep the tone warm, supportive, and highly conversational.

Transactions List:
{txs_formatted}

Return a JSON object conforming exactly to this structure:
{{
  "summary": "...",
  "insights": ["..."],
  "anomalies": ["..."],
  "wasteful_spending": ["..."],
  "highest_spending_day": "...",
  "trends": ["..."],
  "saving_recommendations": ["..."],
  "financial_score": 80
}}

Return ONLY the raw JSON object. Do not wrap in markdown tags.
"""
                raw_json = self._call_custom_llm(prompt, system_prompt).strip()
                if raw_json.startswith("```"):
                    lines = raw_json.split("\n")
                    if lines[0].startswith("```"):
                        lines = lines[1:]
                    if lines[-1].startswith("```"):
                        lines = lines[:-1]
                    raw_json = "\n".join(lines).strip()
                    
                logger.info(f"Custom LLM analyze response: {raw_json}")
                analysis_data = json.loads(raw_json)
                return AnalyzeResponse(**analysis_data)
            except Exception as e:
                logger.error(f"Custom LLM analyze failed: {e}. Falling back to rule-based analyzer.")
                analysis_data = fallback_analyze(transactions)
                return AnalyzeResponse(**analysis_data)

        try:
            tx_lines = []
            for tx in transactions:
                date_val = tx.get("transaction_date") or tx.get("created_at") or "unknown"
                tx_lines.append(
                    f"- [{date_val}] {tx.get('type')} | {tx.get('category')} | Amount: {tx.get('amount')} | Description: {tx.get('description')}"
                )
            txs_formatted = "\n".join(tx_lines)

            prompt = f"""
Analyze the following list of user transactions and generate spending analysis metrics:
1. summary: A friendly paragraph summarizing their financial health and spending patterns in casual Indonesian.
2. insights: List of observations and tips.
3. anomalies: List of unusually high or sudden spikes in spending.
4. wasteful_spending: List of frequent small or unnecessary spending items (like ordering coffee/camilan too often).
5. highest_spending_day: The single day with the highest total expenses, formatted nicely (e.g. "Senin, 15 Jun 2026 sebesar Rp 500.000").
6. trends: Any category spending trend increases or decreases.
7. saving_recommendations: Practical saving recommendations.
8. financial_score: An integer from 0 to 100 based on their savings rate, budgeting, and habits.

**Crucial Constraint**: Write all output text values (summary, insights, anomalies, wasteful_spending, trends, saving_recommendations) in informal/casual Indonesian (Bahasa Indonesia gaul/santai, using terms like "lu", "gua", "nih", "bro", "sist", "lho", "coba deh", "yuk", "boncos", "hemat"). Speak like a close supportive friend advising them on their money. Keep the tone warm, supportive, and highly conversational.

Transactions List:
{txs_formatted}

Return a JSON object conforming exactly to this structure:
{{
  "summary": "...",
  "insights": ["..."],
  "anomalies": ["..."],
  "wasteful_spending": ["..."],
  "highest_spending_day": "...",
  "trends": ["..."],
  "saving_recommendations": ["..."],
  "financial_score": 80
}}

Return ONLY the JSON object. Do not wrap in markdown tags.
"""
            model = genai.GenerativeModel(self.model_name)
            response = model.generate_content(
                prompt,
                generation_config=genai.types.GenerationConfig(
                    response_mime_type="application/json",
                    response_schema=clean_gemini_schema(AnalyzeResponse),
                )
            )
            
            raw_json = response.text.strip()
            logger.info(f"Gemini analyze response: {raw_json}")
            
            analysis_data = json.loads(raw_json)
            return AnalyzeResponse(**analysis_data)
            
        except Exception as e:
            logger.error(f"Gemini API analyze failed: {e}. Falling back to rule-based analyzer.")
            analysis_data = fallback_analyze(transactions)
            return AnalyzeResponse(**analysis_data)
