"""
Unit tests for ParserService.analyze_cashflow.
Runs without a live Gemini API key by exercising the built-in fallback path.
"""
import pytest
from parser_service import ParserService, CashflowInsightResponse


def make_service() -> ParserService:
    """Return a ParserService instance with Gemini intentionally NOT configured."""
    svc = ParserService()
    svc.is_configured = False
    svc.is_custom_llm_configured = False
    return svc


BASE = dict(
    available_balance=5_200_000,
    daily_burn_rate=185_000,
    projected_expense=3_885_000,
    upcoming_obligations=1_150_000,
    target_date="2026-07-25",
    top_categories=["food", "transport"],
)


def call(risk_level: str, projected_balance: int) -> CashflowInsightResponse:
    svc = make_service()
    return svc.analyze_cashflow(
        **BASE,
        projected_balance=projected_balance,
        risk_level=risk_level,
    )


class TestAnalyzeCashflowFallback:
    def test_returns_cashflow_insight_response(self):
        result = call("safe", 2_000_000)
        assert isinstance(result, CashflowInsightResponse)

    def test_always_three_recommendations(self):
        for risk, balance in [("safe", 2_000_000), ("risky", 165_000), ("deficit", -500_000)]:
            result = call(risk, balance)
            assert len(result.recommendations) == 3, f"Expected 3 recs for risk={risk}"

    def test_summary_contains_available_balance(self):
        result = call("safe", 2_000_000)
        assert "5.200.000" in result.summary

    def test_summary_contains_target_date(self):
        result = call("safe", 2_000_000)
        assert "2026-07-25" in result.summary

    def test_risky_first_recommendation_mentions_pengeluaran(self):
        result = call("risky", 165_000)
        first_rec = result.recommendations[0].lower()
        assert "pengeluaran" in first_rec

    def test_deficit_first_recommendation_warns_negative(self):
        result = call("deficit", -300_000)
        first_rec = result.recommendations[0].lower()
        assert any(w in first_rec for w in ["negatif", "penting", "darurat"])

    def test_deficit_summary_contains_warning(self):
        result = call("deficit", -300_000)
        assert "⚠️" in result.summary or "kritis" in result.summary.lower()

    def test_safe_recommendation_positive_tone(self):
        result = call("safe", 2_000_000)
        first_rec = result.recommendations[0].lower()
        assert any(w in first_rec for w in ["pertahankan", "baik", "terjaga", "tabungan", "investasi"])

    def test_zero_balance_treated_as_deficit(self):
        result = call("risky", 0)
        first_rec = result.recommendations[0].lower()
        assert any(w in first_rec for w in ["negatif", "penting", "darurat", "kurangi"])

    def test_empty_categories_does_not_raise(self):
        svc = make_service()
        result = svc.analyze_cashflow(
            available_balance=1_000_000,
            daily_burn_rate=50_000,
            projected_expense=1_000_000,
            upcoming_obligations=0,
            projected_balance=0,
            risk_level="deficit",
            target_date="2026-07-31",
            top_categories=[],
        )
        assert isinstance(result, CashflowInsightResponse)
        assert len(result.recommendations) == 3
