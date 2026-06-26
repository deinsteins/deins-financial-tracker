-- Seed Users
INSERT INTO users (id, telegram_id, full_name, monthly_budget) VALUES
('e8a3a2a6-df6b-4e1b-90f7-66cbb41e9ab8', 123456789, 'Alice Smith', 500000), -- Budget: 500,000 cents ($5,000)
('8f8303f2-1b15-46b5-9005-4b2a3cd0589d', 987654321, 'Bob Jones', 250000)   -- Budget: 250,000 cents ($2,500)
ON CONFLICT (telegram_id) DO NOTHING;

-- Seed Transactions (linked to static User UUIDs)
INSERT INTO transactions (id, user_id, type, category, amount, description, transaction_date) VALUES
-- Alice's Transactions
('9be8d8b9-52e6-42ea-a4e9-86cb493be81a', 'e8a3a2a6-df6b-4e1b-90f7-66cbb41e9ab8', 'income', 'Salary', 300000, 'Biweekly Paycheck', CURRENT_TIMESTAMP - INTERVAL '5 days'),
('b0849202-b258-45a7-ab99-d41935e4d2bf', 'e8a3a2a6-df6b-4e1b-90f7-66cbb41e9ab8', 'expense', 'Food & Dining', 1550, 'Lunch at Cafe', CURRENT_TIMESTAMP - INTERVAL '2 days'),
('c48f2203-b0eb-485a-8b1e-289d023f009e', 'e8a3a2a6-df6b-4e1b-90f7-66cbb41e9ab8', 'expense', 'Transport', 2500, 'Uber Ride', CURRENT_TIMESTAMP - INTERVAL '1 day'),

-- Bob's Transactions
('d29a0090-349f-43b8-80f0-3cd125e9821d', '8f8303f2-1b15-46b5-9005-4b2a3cd0589d', 'expense', 'Housing & Utilities', 120000, 'Monthly Rent', CURRENT_TIMESTAMP - INTERVAL '10 days'),
('f82b7cf2-76bf-4e00-8800-090c2e3a89e9', '8f8303f2-1b15-46b5-9005-4b2a3cd0589d', 'expense', 'Entertainment', 4500, 'Cinema tickets', CURRENT_TIMESTAMP - INTERVAL '3 days')
ON CONFLICT (id) DO NOTHING;

-- Seed Reports
INSERT INTO reports (id, user_id, report_type, content) VALUES
('fa82260f-e22a-4318-8f83-d56eeeb21c7d', 'e8a3a2a6-df6b-4e1b-90f7-66cbb41e9ab8', 'monthly_summary', '{"month": "June 2026", "total_income": 300000, "total_expenses": 4050, "net_savings": 295950}')
ON CONFLICT (id) DO NOTHING;
