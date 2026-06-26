DROP INDEX IF EXISTS idx_reports_user_id;
DROP INDEX IF EXISTS idx_transactions_transaction_date;
DROP INDEX IF EXISTS idx_transactions_user_id;
DROP INDEX IF EXISTS idx_users_telegram_id;

DROP TABLE IF EXISTS reports;
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS users;
