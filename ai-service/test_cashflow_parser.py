"""
Unit tests for ParserService.parse_cashflow (fallback path — no Gemini key needed).
All date resolution logic is fully deterministic, so we pin a fixed reference_date
for reproducible assertions.
"""
import re
from datetime import datetime, timedelta
import calendar
from zoneinfo import ZoneInfo

from parser_service import ParserService, ParsedCashflow

JAKARTA_TZ = ZoneInfo("Asia/Jakarta")
# Fixed reference: 2 July 2026 (mid-month, before 25th payday)
REF_DATE = datetime(2026, 7, 2, 10, 0, 0, tzinfo=JAKARTA_TZ)


def make_service() -> ParserService:
    svc = ParserService()
    svc.is_configured = False
    svc.is_custom_llm_configured = False
    return svc


def parse(text: str) -> ParsedCashflow:
    return make_service().parse_cashflow(text, reference_date=REF_DATE)


# ---------------------------------------------------------------------------
class TestShowCashflowPayday:
    def test_gajian_generic_returns_show_cashflow_payday(self):
        r = parse("uang saya cukup sampai gajian gak?")
        assert r.intent == "show_cashflow"
        assert r.target_type == "payday"

    def test_sampai_gajian_detected(self):
        r = parse("kira-kira sampai gajian masih cukup gak?")
        assert r.intent == "show_cashflow"
        assert r.target_type == "payday"


class TestShowCashflowDays:
    def test_30_hari_ke_depan(self):
        r = parse("prediksi cashflow 30 hari ke depan")
        assert r.intent == "show_cashflow"
        assert r.target_type == "days"
        assert r.target_days == 30

    def test_target_days_resolves_date(self):
        r = parse("prediksi cashflow 30 hari ke depan")
        expected = (REF_DATE + timedelta(days=30)).strftime("%Y-%m-%d")
        assert r.resolved_target_date == expected

    def test_14_hari(self):
        r = parse("proyeksi 14 hari")
        assert r.intent == "show_cashflow"
        assert r.target_days == 14


class TestShowCashflowEndOfMonth:
    def test_akhir_bulan_detected(self):
        r = parse("saldo akhir bulan kira-kira berapa?")
        assert r.intent == "show_cashflow"
        assert r.target_type == "end_of_month"

    def test_end_of_month_resolves_to_last_day(self):
        r = parse("saldo akhir bulan kira-kira berapa?")
        _, last_day = calendar.monthrange(REF_DATE.year, REF_DATE.month)
        expected = REF_DATE.replace(day=last_day).strftime("%Y-%m-%d")
        assert r.resolved_target_date == expected

    def test_bulan_ini_detected(self):
        r = parse("perkiraan cashflow bulan ini")
        assert r.intent == "show_cashflow"
        assert r.target_type == "end_of_month"


class TestSetPayday:
    def test_gajian_tanggal_25(self):
        r = parse("gajian saya tanggal 25")
        assert r.intent == "set_payday"
        assert r.payday_day == 25

    def test_gaji_tgl_10(self):
        r = parse("gaji saya tgl 10")
        assert r.intent == "set_payday"
        assert r.payday_day == 10

    def test_set_payday_resolved_date_is_none(self):
        # set_payday does not resolve a target date (no projection needed)
        r = parse("gajian saya tanggal 25")
        assert r.resolved_target_date is None


class TestUnknownIntent:
    def test_irrelevant_query_returns_unknown(self):
        r = parse("apa menu makan siang yang enak?")
        assert r.intent == "unknown"
        assert r.reason is not None and len(r.reason) > 0

    def test_unknown_has_reason_in_indonesian(self):
        r = parse("cerita dongeng si kancil")
        assert r.intent == "unknown"
        assert r.reason  # not empty


class TestValidation:
    def test_returns_parsed_cashflow_instance(self):
        r = parse("prediksi cashflow 7 hari")
        assert isinstance(r, ParsedCashflow)

    def test_invalid_target_type_falls_back_gracefully(self):
        # Directly construct with bad target_type — validator should coerce to None
        p = ParsedCashflow(intent="show_cashflow", target_type="gibberish")
        assert p.target_type is None

    def test_negative_target_days_becomes_none(self):
        p = ParsedCashflow(intent="show_cashflow", target_days=-5)
        assert p.target_days is None

    def test_zero_payday_day_becomes_none(self):
        p = ParsedCashflow(intent="set_payday", payday_day=0)
        assert p.payday_day is None

    def test_unknown_intent_string_coerced(self):
        p = ParsedCashflow(intent="definitely_not_valid")
        assert p.intent == "unknown"
