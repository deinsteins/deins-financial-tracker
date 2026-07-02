package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ==========================================
// 1. save_transaction
// ==========================================

// SaveTransactionArgs is the arguments for the save_transaction tool.
type SaveTransactionArgs struct {
	Type        string `json:"type"`
	Amount      int64  `json:"amount"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

type SaveTransactionTool struct {
	Handler func(ctx context.Context, telegramID int64, args *SaveTransactionArgs) (interface{}, error)
}

func (t *SaveTransactionTool) Name() string { return "save_transaction" }
func (t *SaveTransactionTool) Description() string {
	return "Save a new transaction (income or expense) into the database."
}

func (t *SaveTransactionTool) Parameters() Parameters {
	return Parameters{
		Type: "object",
		Properties: map[string]Property{
			"type": {
				Type:        "string",
				Description: "The type of transaction: 'income' for earnings/salary, 'expense' for spendings/bills.",
				Enum:        []string{"income", "expense"},
			},
			"amount": {
				Type:        "integer",
				Description: "The amount of money in the transaction. Always convert slang like 25rb to 25000, 1.5jt to 1500000.",
			},
			"category": {
				Type:        "string",
				Description: "The category of the transaction. Must be food, groceries, shopping, transport, utilities, entertainment, salary, or other.",
				Enum:        []string{"food", "groceries", "shopping", "transport", "utilities", "entertainment", "salary", "other"},
			},
			"description": {
				Type:        "string",
				Description: "A description of what the money was spent on or received from.",
			},
		},
		Required: []string{"type", "amount", "category", "description"},
	}
}

func (t *SaveTransactionTool) Validate(argsRaw string) (interface{}, error) {
	var args SaveTransactionArgs
	if argsRaw == "" {
		return nil, errors.New("missing arguments payload")
	}
	if err := json.Unmarshal([]byte(argsRaw), &args); err != nil {
		return nil, fmt.Errorf("invalid JSON payload: %w", err)
	}

	if args.Type != "income" && args.Type != "expense" {
		return nil, fmt.Errorf("invalid transaction type: must be 'income' or 'expense'")
	}
	if args.Amount <= 0 {
		return nil, fmt.Errorf("invalid amount: must be positive, got %d", args.Amount)
	}
	validCategories := map[string]bool{
		"food": true, "groceries": true, "shopping": true,
		"transport": true, "utilities": true,
		"entertainment": true, "salary": true, "other": true,
	}
	if !validCategories[args.Category] {
		return nil, fmt.Errorf("invalid category: must be one of food, groceries, shopping, transport, utilities, entertainment, salary, other")
	}
	if args.Description == "" {
		return nil, fmt.Errorf("invalid description: cannot be empty")
	}

	return &args, nil
}

func (t *SaveTransactionTool) Execute(ctx context.Context, telegramID int64, argsRaw string) (interface{}, error) {
	parsed, err := t.Validate(argsRaw)
	if err != nil {
		return nil, err
	}
	if t.Handler == nil {
		return nil, errors.New("execute handler not configured")
	}
	return t.Handler(ctx, telegramID, parsed.(*SaveTransactionArgs))
}

// ==========================================
// 2. get_today_summary
// ==========================================

type GetTodaySummaryTool struct {
	Handler func(ctx context.Context, telegramID int64) (interface{}, error)
}

func (t *GetTodaySummaryTool) Name() string { return "get_today_summary" }
func (t *GetTodaySummaryTool) Description() string {
	return "Retrieve today's financial summary or general daily report."
}

func (t *GetTodaySummaryTool) Parameters() Parameters {
	return Parameters{
		Type:       "object",
		Properties: map[string]Property{},
	}
}

func (t *GetTodaySummaryTool) Validate(argsRaw string) (interface{}, error) {
	return nil, nil // No validation needed
}

func (t *GetTodaySummaryTool) Execute(ctx context.Context, telegramID int64, argsRaw string) (interface{}, error) {
	if t.Handler == nil {
		return nil, errors.New("execute handler not configured")
	}
	return t.Handler(ctx, telegramID)
}

// ==========================================
// 3. get_month_summary
// ==========================================

type GetMonthSummaryTool struct {
	Handler func(ctx context.Context, telegramID int64) (interface{}, error)
}

func (t *GetMonthSummaryTool) Name() string { return "get_month_summary" }
func (t *GetMonthSummaryTool) Description() string {
	return "Retrieve the monthly financial report or monthly summary."
}

func (t *GetMonthSummaryTool) Parameters() Parameters {
	return Parameters{
		Type:       "object",
		Properties: map[string]Property{},
	}
}

func (t *GetMonthSummaryTool) Validate(argsRaw string) (interface{}, error) {
	return nil, nil
}

func (t *GetMonthSummaryTool) Execute(ctx context.Context, telegramID int64, argsRaw string) (interface{}, error) {
	if t.Handler == nil {
		return nil, errors.New("execute handler not configured")
	}
	return t.Handler(ctx, telegramID)
}

// ==========================================
// 4. get_transactions
// ==========================================

// GetTransactionsArgs is the arguments for the get_transactions tool.
type GetTransactionsArgs struct {
	Limit int    `json:"limit,omitempty"`
	Type  string `json:"type,omitempty"`
}

type GetTransactionsTool struct {
	Handler func(ctx context.Context, telegramID int64, args *GetTransactionsArgs) (interface{}, error)
}

func (t *GetTransactionsTool) Name() string { return "get_transactions" }
func (t *GetTransactionsTool) Description() string {
	return "Retrieve a list of recent transactions."
}

func (t *GetTransactionsTool) Parameters() Parameters {
	return Parameters{
		Type: "object",
		Properties: map[string]Property{
			"limit": {
				Type:        "integer",
				Description: "Maximum number of transactions to retrieve. Defaults to 10.",
			},
			"type": {
				Type:        "string",
				Description: "Filter by transaction type: 'income' or 'expense'. Optional.",
				Enum:        []string{"income", "expense"},
			},
		},
	}
}

func (t *GetTransactionsTool) Validate(argsRaw string) (interface{}, error) {
	var args GetTransactionsArgs
	if argsRaw != "" && argsRaw != "{}" {
		if err := json.Unmarshal([]byte(argsRaw), &args); err != nil {
			return nil, fmt.Errorf("invalid JSON payload: %w", err)
		}
	}
	if args.Limit < 0 {
		return nil, fmt.Errorf("invalid limit: must be greater than or equal to 0, got %d", args.Limit)
	}
	if args.Limit == 0 {
		args.Limit = 10
	}
	if args.Type != "" && args.Type != "income" && args.Type != "expense" {
		return nil, fmt.Errorf("invalid transaction type filter: must be 'income' or 'expense'")
	}
	return &args, nil
}

func (t *GetTransactionsTool) Execute(ctx context.Context, telegramID int64, argsRaw string) (interface{}, error) {
	parsed, err := t.Validate(argsRaw)
	if err != nil {
		return nil, err
	}
	if t.Handler == nil {
		return nil, errors.New("execute handler not configured")
	}
	return t.Handler(ctx, telegramID, parsed.(*GetTransactionsArgs))
}

// ==========================================
// 5. analyze_spending
// ==========================================

type AnalyzeSpendingTool struct {
	Handler func(ctx context.Context, telegramID int64) (interface{}, error)
}

func (t *AnalyzeSpendingTool) Name() string { return "analyze_spending" }
func (t *AnalyzeSpendingTool) Description() string {
	return "Generate AI analysis and financial health tips/insights based on monthly transactions."
}

func (t *AnalyzeSpendingTool) Parameters() Parameters {
	return Parameters{
		Type:       "object",
		Properties: map[string]Property{},
	}
}

func (t *AnalyzeSpendingTool) Validate(argsRaw string) (interface{}, error) {
	return nil, nil
}

func (t *AnalyzeSpendingTool) Execute(ctx context.Context, telegramID int64, argsRaw string) (interface{}, error) {
	if t.Handler == nil {
		return nil, errors.New("execute handler not configured")
	}
	return t.Handler(ctx, telegramID)
}

// ==========================================
// 6. set_monthly_budget
// ==========================================

type SetMonthlyBudgetArgs struct {
	Amount int64 `json:"amount"`
}

type SetMonthlyBudgetTool struct {
	Handler func(ctx context.Context, telegramID int64, args *SetMonthlyBudgetArgs) (interface{}, error)
}

func (t *SetMonthlyBudgetTool) Name() string { return "set_monthly_budget" }
func (t *SetMonthlyBudgetTool) Description() string {
	return "Set or update the user's total monthly spending budget limit."
}
func (t *SetMonthlyBudgetTool) Parameters() Parameters {
	return Parameters{
		Type: "object",
		Properties: map[string]Property{
			"amount": {
				Type:        "integer",
				Description: "The monthly budget limit amount. Always convert slang like 5jt to 5000000.",
			},
		},
		Required: []string{"amount"},
	}
}
func (t *SetMonthlyBudgetTool) Validate(argsRaw string) (interface{}, error) {
	var args SetMonthlyBudgetArgs
	if argsRaw == "" {
		return nil, errors.New("missing arguments payload")
	}
	if err := json.Unmarshal([]byte(argsRaw), &args); err != nil {
		return nil, fmt.Errorf("invalid JSON payload: %w", err)
	}
	if args.Amount <= 0 {
		return nil, fmt.Errorf("invalid budget amount: must be positive, got %d", args.Amount)
	}
	return &args, nil
}
func (t *SetMonthlyBudgetTool) Execute(ctx context.Context, telegramID int64, argsRaw string) (interface{}, error) {
	parsed, err := t.Validate(argsRaw)
	if err != nil {
		return nil, err
	}
	if t.Handler == nil {
		return nil, errors.New("execute handler not configured")
	}
	return t.Handler(ctx, telegramID, parsed.(*SetMonthlyBudgetArgs))
}

// ==========================================
// 7. set_category_budget
// ==========================================

type SetCategoryBudgetArgs struct {
	Category string `json:"category"`
	Amount   int64  `json:"amount"`
}

type SetCategoryBudgetTool struct {
	Handler func(ctx context.Context, telegramID int64, args *SetCategoryBudgetArgs) (interface{}, error)
}

func (t *SetCategoryBudgetTool) Name() string { return "set_category_budget" }
func (t *SetCategoryBudgetTool) Description() string {
	return "Set or update the user's spending budget limit for a specific category."
}
func (t *SetCategoryBudgetTool) Parameters() Parameters {
	return Parameters{
		Type: "object",
		Properties: map[string]Property{
			"category": {
				Type:        "string",
				Description: "The category for the budget. Must be food, groceries, shopping, transport, utilities, entertainment, salary, or other.",
				Enum:        []string{"food", "groceries", "shopping", "transport", "utilities", "entertainment", "salary", "other"},
			},
			"amount": {
				Type:        "integer",
				Description: "The budget limit amount for the category. Always convert slang like 500rb to 500000.",
			},
		},
		Required: []string{"category", "amount"},
	}
}
func (t *SetCategoryBudgetTool) Validate(argsRaw string) (interface{}, error) {
	var args SetCategoryBudgetArgs
	if argsRaw == "" {
		return nil, errors.New("missing arguments payload")
	}
	if err := json.Unmarshal([]byte(argsRaw), &args); err != nil {
		return nil, fmt.Errorf("invalid JSON payload: %w", err)
	}
	validCategories := map[string]bool{
		"food": true, "groceries": true, "shopping": true,
		"transport": true, "utilities": true,
		"entertainment": true, "salary": true, "other": true,
	}
	if !validCategories[args.Category] {
		return nil, fmt.Errorf("invalid category: must be one of food, groceries, shopping, transport, utilities, entertainment, salary, other")
	}
	if args.Amount <= 0 {
		return nil, fmt.Errorf("invalid budget amount: must be positive, got %d", args.Amount)
	}
	return &args, nil
}
func (t *SetCategoryBudgetTool) Execute(ctx context.Context, telegramID int64, argsRaw string) (interface{}, error) {
	parsed, err := t.Validate(argsRaw)
	if err != nil {
		return nil, err
	}
	if t.Handler == nil {
		return nil, errors.New("execute handler not configured")
	}
	return t.Handler(ctx, telegramID, parsed.(*SetCategoryBudgetArgs))
}

// ==========================================
// 8. delete_transaction
// ==========================================

type DeleteTransactionArgs struct {
	ID   string `json:"id,omitempty"`
	Last bool   `json:"last,omitempty"`
}

type DeleteTransactionTool struct {
	Handler func(ctx context.Context, telegramID int64, args *DeleteTransactionArgs) (interface{}, error)
}

func (t *DeleteTransactionTool) Name() string { return "delete_transaction" }
func (t *DeleteTransactionTool) Description() string {
	return "Delete, undo, or cancel a transaction. Can target the last transaction (set last=true) or a specific transaction ID."
}

func (t *DeleteTransactionTool) Parameters() Parameters {
	return Parameters{
		Type: "object",
		Properties: map[string]Property{
			"id": {
				Type:        "string",
				Description: "The UUID string of the transaction to delete.",
			},
			"last": {
				Type:        "boolean",
				Description: "Set to true to delete the user's latest transaction.",
			},
		},
	}
}

func (t *DeleteTransactionTool) Validate(argsRaw string) (interface{}, error) {
	var args DeleteTransactionArgs
	if argsRaw != "" && argsRaw != "{}" {
		if err := json.Unmarshal([]byte(argsRaw), &args); err != nil {
			return nil, fmt.Errorf("invalid JSON payload: %w", err)
		}
	}
	return &args, nil
}

func (t *DeleteTransactionTool) Execute(ctx context.Context, telegramID int64, argsRaw string) (interface{}, error) {
	parsed, err := t.Validate(argsRaw)
	if err != nil {
		return nil, err
	}
	if t.Handler == nil {
		return nil, errors.New("execute handler not configured")
	}
	return t.Handler(ctx, telegramID, parsed.(*DeleteTransactionArgs))
}
