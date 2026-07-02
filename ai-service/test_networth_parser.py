import unittest
from parser_service import (
    normalize_indonesian_currency,
    fallback_parse_networth,
    ParsedNetWorth
)

class TestNetWorthParser(unittest.TestCase):
    def test_normalize_currency(self):
        self.assertEqual(normalize_indonesian_currency("12jt"), "12000000")
        self.assertEqual(normalize_indonesian_currency("12 juta"), "12000000")
        self.assertEqual(normalize_indonesian_currency("500rb"), "500000")
        self.assertEqual(normalize_indonesian_currency("500 ribu"), "500000")
        self.assertEqual(normalize_indonesian_currency("50k", support_k=True), "50000")

    def test_fallback_parse_networth(self):
        # 1. Add asset bank
        res = fallback_parse_networth("saldo BCA saya 12 juta")
        self.assertEqual(res["intent"], "add_asset")
        self.assertEqual(res["type"], "bank")
        self.assertEqual(res["name"], "BCA")
        self.assertEqual(res["amount"], 12000000)

        # 2. Add asset cash
        res = fallback_parse_networth("cash di dompet 500rb")
        self.assertEqual(res["intent"], "add_asset")
        self.assertEqual(res["type"], "cash")
        self.assertEqual(res["name"], "Dompet")
        self.assertEqual(res["amount"], 500000)

        # 3. Add asset investment
        res = fallback_parse_networth("saham saya sekarang 5 juta")
        self.assertEqual(res["intent"], "add_asset")
        self.assertEqual(res["type"], "investment")
        self.assertEqual(res["name"], "Saham")
        self.assertEqual(res["amount"], 5000000)

        # 4. Add liability
        res = fallback_parse_networth("cicilan motor sisa 2 juta")
        self.assertEqual(res["intent"], "add_liability")
        self.assertEqual(res["type"], "loan")
        self.assertEqual(res["name"], "Cicilan Motor")
        self.assertEqual(res["amount"], 2000000)

        # 5. Update asset
        res = fallback_parse_networth("update saldo BCA jadi 15 juta")
        self.assertEqual(res["intent"], "update_asset")
        self.assertEqual(res["type"], "bank")
        self.assertEqual(res["name"], "BCA")
        self.assertEqual(res["amount"], 15000000)

        # 6. Delete asset
        res = fallback_parse_networth("hapus aset BCA")
        self.assertEqual(res["intent"], "delete_asset")
        self.assertEqual(res["name"], "BCA")

        # 7. Show networth
        res = fallback_parse_networth("tampilkan total kekayaan")
        self.assertEqual(res["intent"], "show_networth")

        # 8. Unknown: Missing name
        res = fallback_parse_networth("update saldo jadi 15 juta")
        self.assertEqual(res["intent"], "unknown")
        self.assertIsNotNone(res["reason"])

        # 9. Unknown: Missing amount
        res = fallback_parse_networth("tambah saldo BCA")
        self.assertEqual(res["intent"], "unknown")
        self.assertIsNotNone(res["reason"])

if __name__ == "__main__":
    unittest.main()
