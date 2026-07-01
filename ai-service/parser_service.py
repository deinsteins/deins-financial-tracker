import os
import re
import json
import logging
import urllib.request
from pydantic import BaseModel, Field
import google.generativeai as genai

logger = logging.getLogger(__name__)

# Pydantic schema for parsing validation
class ParsedTransaction(BaseModel):
    type: str = Field(..., description="Must be 'expense' or 'income'")
    category: str = Field(..., description="Normalized category, e.g., 'food', 'transport', 'utilities', 'entertainment', 'salary', 'other'")
    amount: int = Field(..., description="Transaction amount as an integer")
    description: str = Field(..., description="Sanitized description of the transaction")

class ReceiptItem(BaseModel):
    name: str = Field(..., description="Item name as printed on the receipt")
    qty: int = Field(default=1, description="Quantity purchased, defaults to 1")
    price: int = Field(..., description="Unit price in IDR as an integer")

class ParsedReceipt(BaseModel):
    merchant: str = Field(default="", description="Store or restaurant name")
    items: list[ReceiptItem] = Field(default=[], description="List of purchased items")
    total: int = Field(default=0, description="Total amount in IDR as an integer")
    date: str | None = Field(default=None, description="ISO 8601 date string or null if not found")

# Pydantic schema for analysis validation
class AnalyzeResponse(BaseModel):
    summary: str = Field(..., description="Concise paragraph summary of the user financial health and spending patterns in casual Indonesian")
    insights: list[str] = Field(..., description="List of actionable insights and observations in casual Indonesian")
    anomalies: list[str] = Field(default=[], description="Detected spending anomalies (unusually high expenses) in casual Indonesian")
    wasteful_spending: list[str] = Field(default=[], description="Detected wasteful spending (frequent small or unnecessary expenses) in casual Indonesian")
    highest_spending_day: str = Field(default="", description="The day with highest spending, formatted nicely (e.g. 'Senin, 15 Jun 2026 sebesar Rp 500.000')")
    trends: list[str] = Field(default=[], description="Category trend increases or decreases compared to previous transactions in casual Indonesian")
    saving_recommendations: list[str] = Field(default=[], description="Actionable saving recommendations in casual Indonesian")
    financial_score: int = Field(default=80, description="Financial score from 0 to 100 based on their savings rate, budgeting, and spending habits")

def normalize_indonesian_currency(text: str) -> str:
    """
    Normalizes Indonesian slang currency formats:
    - rb / ribu -> 1,000
    - jt / juta -> 1,000,000
    Handles decimal commas (e.g. 2,5jt -> 2.5jt -> 2500000)
    """
    if not text:
        return ""
    
    # 1. Replace decimal commas with dots if followed by currency multiplier words
    # e.g., 2,5jt -> 2.5jt
    normalized = re.sub(
        r'(\d+),(\d+)(\s*(?:rb|ribu|jt|juta)\b)',
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
    
    return normalized

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
    if any(w in lower_text for w in ["makan", "minum", "bakso", "kopi", "warteg", "food", "dining", "cafe", "restoran"]):
        category = "food"
    elif any(w in lower_text for w in ["ojek", "uber", "gojek", "grab", "bensin", "transport", "mrt", "bus", "kereta"]):
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
2. "category": a normalized category name (lowercase, e.g., "food", "transport", "utilities", "entertainment", "salary", "other").
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
2. "category": a normalized category name (lowercase, e.g., "food", "transport", "utilities", "entertainment", "salary", "other").
3. "amount": the transaction amount as an integer.
4. "description": what the transaction was for (exclude the amount or currency symbols, and clean up into friendly informal Indonesian if necessary).

Return ONLY a JSON object. Do not wrap in markdown tags like ```json.
"""
            model = genai.GenerativeModel(self.model_name)
            response = model.generate_content(
                prompt,
                generation_config=genai.types.GenerationConfig(
                    response_mime_type="application/json",
                    response_schema=ParsedTransaction,
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
                        response_schema=ParsedReceipt,
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
                    response_schema=AnalyzeResponse,
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
