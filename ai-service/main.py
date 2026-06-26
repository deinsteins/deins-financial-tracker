import os
from datetime import datetime
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from parser_service import ParserService, ParsedTransaction, AnalyzeResponse

app = FastAPI(
    title="Finance Assistant AI Service",
    description="AI Service for parsing transactions and generating budget suggestions",
    version="1.0.0"
)

# Initialize parsing service
parser_service = ParserService()

class ParseRequest(BaseModel):
    text: str = Field(..., example="makan bakso 25rb")

class TransactionItem(BaseModel):
    type: str = Field(..., example="expense")
    category: str = Field(..., example="food")
    amount: int = Field(..., example=25000)
    description: str = Field(..., example="makan bakso")
    transaction_date: str = Field(None, example="2026-06-26T11:15:00Z")

class AnalyzeRequest(BaseModel):
    transactions: list[TransactionItem]

@app.get("/")
def read_root():
    return {
        "service": "Finance Assistant AI Service",
        "status": "running",
        "endpoints": {
            "health": "/health",
            "parse": "/parse",
            "analyze": "/analyze"
        }
    }

@app.get("/health")
def health_check():
    return {
        "status": "ok",
        "service": "ai-service",
        "time": datetime.utcnow().isoformat(),
        "gemini_configured": parser_service.is_configured,
        "database_url_configured": os.getenv("DATABASE_URL") is not None
    }

@app.post("/parse", response_model=ParsedTransaction)
def parse_transaction(request: ParseRequest):
    if not request.text.strip():
        raise HTTPException(status_code=400, detail="Text field cannot be empty.")
    
    try:
        parsed_result = parser_service.parse_transaction(request.text)
        return parsed_result
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to parse transaction: {str(e)}")

@app.post("/analyze", response_model=AnalyzeResponse)
def analyze_transactions(request: AnalyzeRequest):
    try:
        # Convert Pydantic objects to plain dictionaries for processing
        transactions_dict = [tx.dict() for tx in request.transactions]
        analysis_result = parser_service.analyze_transactions(transactions_dict)
        return analysis_result
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to analyze transactions: {str(e)}")
