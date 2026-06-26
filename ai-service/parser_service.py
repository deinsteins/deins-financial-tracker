import os
import re
import json
import logging
from pydantic import BaseModel, Field
import google.generativeai as genai

logger = logging.getLogger(__name__)

# Pydantic schema for parsing validation
class ParsedTransaction(BaseModel):
    type: str = Field(..., description="Must be 'expense' or 'income'")
    category: str = Field(..., description="Normalized category, e.g., 'food', 'transport', 'utilities', 'entertainment', 'salary', 'other'")
    amount: int = Field(..., description="Transaction amount as an integer")
    description: str = Field(..., description="Sanitized description of the transaction")

# Pydantic schema for analysis validation
class AnalyzeResponse(BaseModel):
    summary: str = Field(..., description="Concise paragraph summary of the user financial health and spending patterns in casual Indonesian")
    insights: list[str] = Field(..., description="List of actionable insights and observations in casual Indonesian")

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
    """
    if not transactions:
        return {
            "summary": "Belum ada catatan riwayat transaksi nih bro buat dianalisis.",
            "insights": ["Yuk langsung catat aja pengeluaran/pemasukan lu biar nanti gua kasih analisis gokil!"]
        }
    
    total_income = sum(tx.get("amount", 0) for tx in transactions if tx.get("type") == "income")
    total_expense = sum(tx.get("amount", 0) for tx in transactions if tx.get("type") == "expense")
    
    cat_expenses = {}
    for tx in transactions:
        if tx.get("type") == "expense":
            cat = tx.get("category", "other")
            cat_expenses[cat] = cat_expenses.get(cat, 0) + tx.get("amount", 0)
            
    biggest_category = "None"
    biggest_amount = 0
    if cat_expenses:
        biggest_category = max(cat_expenses, key=cat_expenses.get)
        biggest_amount = cat_expenses[biggest_category]
        
    summary = (
        f"Nih ringkasan dana lu bro: total pemasukan lu ada Rp {total_income:,}, terus pengeluaran lu totalnya Rp {total_expense:,}. "
        f"Nah, pengeluaran lu paling banyak boncos di kategori '{biggest_category}' yaitu Rp {biggest_amount:,}."
    )
    
    insights = []
    insights.append(f"Pola belanja lu nih: Area jajan paling gede ada di '{biggest_category}' (Rp {biggest_amount:,}). Coba agak dikontrol ya bro!")
    
    if total_expense > total_income:
        insights.append("Duh bahaya nih! Pengeluaran lu lebih gede dari pemasukan bulan ini. Coba kurangin belanjaan yang kurang penting, yuk bisa yuk!")
    else:
        savings = total_income - total_expense
        savings_rate = int((savings / total_income) * 100) if total_income > 0 else 0
        insights.append(f"Tips hemat: Lu berhasil nabung Rp {savings:,} (savings rate: {savings_rate}%). Gokil! Coba deh otomatisasi sisihin minimal 10% di awal pas gajian.")
        
    # Check for large transactions (e.g. > 500,000)
    large_txs = [tx for tx in transactions if tx.get("type") == "expense" and tx.get("amount", 0) > 500000]
    if large_txs:
        tx_desc = [f"'{tx.get('description')}' (Rp {tx.get('amount'):,})" for tx in large_txs]
        insights.append(f"Deteksi aneh nih: Ada transaksi lumayan gede nih bro: {', '.join(tx_desc)}.")
        
    return {
        "summary": summary,
        "insights": insights
    }

class ParserService:
    def __init__(self):
        self.api_key = os.getenv("GEMINI_API_KEY")
        self.model_name = os.getenv("GEMINI_MODEL", "gemini-1.5-flash")
        self.is_configured = False
        
        if self.api_key and self.api_key != "YOUR_GEMINI_API_KEY_HERE":
            try:
                genai.configure(api_key=self.api_key)
                self.is_configured = True
                logger.info("Gemini API initialized successfully.")
            except Exception as e:
                logger.error(f"Failed to configure Gemini API: {e}")
        else:
            logger.warning("Gemini API Key is not set or is placeholder. Falling back to rule-based parser/analyzer.")

    def parse_transaction(self, text: str) -> ParsedTransaction:
        preprocessed_text = normalize_indonesian_currency(text)
        logger.info(f"Original text: '{text}' | Preprocessed text: '{preprocessed_text}'")

        if not self.is_configured:
            logger.info("Using local fallback rule-based parser.")
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

    def analyze_transactions(self, transactions: list) -> AnalyzeResponse:
        logger.info(f"Analyzing {len(transactions)} transactions")

        if not self.is_configured:
            logger.info("Using local fallback rule-based analyzer.")
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
You are a expert personal finance advisor. Analyze the following list of user transactions and generate:
1. Spending pattern analysis
2. Biggest expense category
3. Unusual spending detection (outliers, sudden jumps, high frequency of specific items)
4. Saving recommendations

**Crucial Constraint**: Write all output values (the "summary" paragraph and each string in the "insights" array) in informal/casual Indonesian (Bahasa Indonesia gaul/santai, using terms like "lu", "gua", "nih", "bro", "sist", "lho", "coba deh", "yuk", "boncos", "hemat"). Speak like a close supportive friend advising them on their money. Keep the tone warm, supportive, and highly conversational.

Transactions List:
{txs_formatted}

Return a JSON object conforming exactly to this structure:
{{
  "summary": "A friendly paragraph summarizing their financial health and spending patterns in casual Indonesian.",
  "insights": [
    "Insight 1: Casual Indonesian observation about spending patterns or the biggest expense category.",
    "Insight 2: Casual Indonesian check/warning about unusual spending or high-cost transactions.",
    "Insight 3: A concrete actionable recommendation in casual Indonesian on how they can save money."
  ]
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
