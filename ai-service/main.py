import io
import os
from datetime import datetime

import pytesseract
from fastapi import FastAPI, File, HTTPException, UploadFile
from PIL import Image, UnidentifiedImageError
from pydantic import BaseModel, Field

from parser_service import (
    ParserService,
    ParsedTransaction,
    AnalyzeResponse,
    ParsedReceipt,
    ReceiptItem,
    ParsedDebt,
    ParsedNetWorth,
    ParsedCashflow,
    CashflowInsightResponse,
    normalize_indonesian_currency,
)

# Allowed image upload types for the OCR endpoint
ALLOWED_OCR_EXTENSIONS = {".jpg", ".jpeg", ".png"}
ALLOWED_OCR_CONTENT_TYPES = {"image/jpeg", "image/png"}
# Maximum accepted upload size for the OCR endpoint (10 MB)
MAX_OCR_BYTES = 10 * 1024 * 1024

app = FastAPI(
    title="Finance Assistant AI Service",
    description="AI Service for parsing transactions and generating budget suggestions",
    version="1.0.0"
)

# Initialize parsing service
parser_service = ParserService()

class ParseRequest(BaseModel):
    text: str = Field(..., example="makan bakso 25rb")

class ParseDebtRequest(BaseModel):
    text: str = Field(..., example="Andi hutang ke saya 200rb buat makan kemarin")

class ParseNetWorthRequest(BaseModel):
    text: str = Field(..., example="saldo BCA saya 12 juta")

class ParseCashflowRequest(BaseModel):
    text: str = Field(..., example="uang saya cukup sampai gajian gak?")

class AnalyzeCashflowRequest(BaseModel):
    available_balance: int = Field(..., example=5200000)
    daily_burn_rate: int = Field(..., example=185000)
    projected_expense: int = Field(..., example=3885000)
    upcoming_obligations: int = Field(default=0, example=1150000)
    projected_balance: int = Field(..., example=165000)
    risk_level: str = Field(..., example="risky")
    target_date: str = Field(..., example="2026-07-25")
    top_categories: list[str] = Field(default=[], example=["food", "transport"])

class TransactionItem(BaseModel):
    type: str = Field(..., example="expense")
    category: str = Field(..., example="food")
    amount: int = Field(..., example=25000)
    description: str = Field(..., example="makan bakso")
    transaction_date: str = Field(None, example="2026-06-26T11:15:00Z")

class AnalyzeRequest(BaseModel):
    transactions: list[TransactionItem]

class OCRReceiptResponse(BaseModel):
    filename: str = Field(..., example="receipt.jpg")
    raw_text: str = Field(..., example="Bakso Pak Kumis\nTotal: 25.000")
    merchant: str = Field(default="", example="Bakso Pak Kumis")
    items: list[ReceiptItem] = Field(default=[])
    total: int = Field(default=0, example=25000)
    date: str | None = Field(default=None, example="2026-07-01")
    category: str = Field(default="other", example="food")

@app.get("/")
def read_root():
    return {
        "service": "Finance Assistant AI Service",
        "status": "running",
        "endpoints": {
            "health": "/health",
            "parse": "/parse",
            "analyze": "/analyze",
            "ocr": "/ocr",
            "parse-debt": "/parse-debt",
            "parse-networth": "/parse-networth",
            "parse-cashflow": "/parse-cashflow",
            "analyze-cashflow": "/analyze-cashflow"
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

@app.post("/parse-debt", response_model=ParsedDebt)
def parse_debt(request: ParseDebtRequest):
    if not request.text.strip():
        raise HTTPException(status_code=400, detail="Text field cannot be empty.")

    try:
        parsed_result = parser_service.parse_debt(request.text)
        return parsed_result
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to parse debt: {str(e)}")

@app.post("/parse-networth", response_model=ParsedNetWorth)
def parse_networth(request: ParseNetWorthRequest):
    if not request.text.strip():
        raise HTTPException(status_code=400, detail="Text field cannot be empty.")

    try:
        parsed_result = parser_service.parse_networth(request.text)
        return parsed_result
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to parse networth: {str(e)}")

@app.post("/parse-cashflow", response_model=ParsedCashflow)
def parse_cashflow(request: ParseCashflowRequest):
    """Parse natural language cashflow prediction intent from Indonesian text."""
    if not request.text.strip():
        raise HTTPException(status_code=400, detail="Text field cannot be empty.")

    try:
        parsed_result = parser_service.parse_cashflow(request.text)
        return parsed_result
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to parse cashflow: {str(e)}")

@app.post("/analyze-cashflow", response_model=CashflowInsightResponse)
def analyze_cashflow(request: AnalyzeCashflowRequest):
    """Generate AI-powered cashflow insight in Indonesian based on prediction data."""
    try:
        result = parser_service.analyze_cashflow(
            available_balance=request.available_balance,
            daily_burn_rate=request.daily_burn_rate,
            projected_expense=request.projected_expense,
            upcoming_obligations=request.upcoming_obligations,
            projected_balance=request.projected_balance,
            risk_level=request.risk_level,
            target_date=request.target_date,
            top_categories=request.top_categories,
        )
        return result
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to analyze cashflow: {str(e)}")

@app.post("/analyze", response_model=AnalyzeResponse)
def analyze_transactions(request: AnalyzeRequest):
    try:
        # Convert Pydantic objects to plain dictionaries for processing
        transactions_dict = [tx.dict() for tx in request.transactions]
        analysis_result = parser_service.analyze_transactions(transactions_dict)
        return analysis_result
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to analyze transactions: {str(e)}")

@app.post("/ocr", response_model=OCRReceiptResponse)
async def extract_text_from_image(file: UploadFile = File(...)):
    # Validate that a filename was provided
    if not file.filename:
        raise HTTPException(status_code=400, detail="No file provided.")

    # Validate the file extension
    extension = os.path.splitext(file.filename)[1].lower()
    if extension not in ALLOWED_OCR_EXTENSIONS:
        raise HTTPException(
            status_code=400,
            detail=(
                "Invalid file type. Only .jpg, .jpeg, and .png files are supported, "
                f"got '{extension or 'unknown'}'."
            ),
        )

    # Validate the declared content type when available
    if file.content_type and file.content_type not in ALLOWED_OCR_CONTENT_TYPES:
        raise HTTPException(
            status_code=400,
            detail=f"Invalid content type '{file.content_type}'. Expected a JPEG or PNG image.",
        )

    # Reject oversized uploads before buffering them into memory
    if file.size is not None and file.size > MAX_OCR_BYTES:
        raise HTTPException(
            status_code=413,
            detail=f"File too large. Maximum allowed size is {MAX_OCR_BYTES // (1024 * 1024)} MB.",
        )

    # Read the uploaded file contents
    try:
        contents = await file.read()
    except Exception as e:
        raise HTTPException(status_code=400, detail=f"Failed to read uploaded file: {str(e)}")
    finally:
        await file.close()

    if not contents:
        raise HTTPException(status_code=400, detail="Uploaded file is empty.")

    # Guard against clients that under-report or omit the declared size
    if len(contents) > MAX_OCR_BYTES:
        raise HTTPException(
            status_code=413,
            detail=f"File too large. Maximum allowed size is {MAX_OCR_BYTES // (1024 * 1024)} MB.",
        )

    # Open and verify the image with Pillow
    try:
        image = Image.open(io.BytesIO(contents))
        image.load()
    except UnidentifiedImageError:
        raise HTTPException(
            status_code=400,
            detail="Uploaded file is not a valid or is a corrupted image.",
        )
    except Exception as e:
        raise HTTPException(status_code=400, detail=f"Failed to process image: {str(e)}")

    # Run OCR text extraction
    try:
        extracted_text = pytesseract.image_to_string(image)
    except pytesseract.TesseractNotFoundError:
        raise HTTPException(
            status_code=500,
            detail="OCR engine (Tesseract) is not installed or not available.",
        )
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to extract text from image: {str(e)}")

    normalized_text = normalize_indonesian_currency(extracted_text)

    try:
        receipt = parser_service.parse_receipt(normalized_text)
    except ValueError as e:
        raise HTTPException(
            status_code=503,
            detail={"error": str(e), "raw_text": extracted_text},
        )
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to extract receipt data: {str(e)}")

    return OCRReceiptResponse(
        filename=file.filename,
        raw_text=extracted_text,
        merchant=receipt.merchant,
        items=receipt.items,
        total=receipt.total,
        date=receipt.date,
        category=receipt.category,
    )
