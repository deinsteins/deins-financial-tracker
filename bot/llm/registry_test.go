package llm

import (
	"context"
	"errors"
	"testing"
)

func TestRegistry_RegisterAndList(t *testing.T) {
	registry := NewRegistry()
	if len(registry.List()) != 0 {
		t.Errorf("expected empty registry, got %d tools", len(registry.List()))
	}

	tool := &GetTodaySummaryTool{
		Handler: func(ctx context.Context, telegramID int64) (interface{}, error) {
			return "summary", nil
		},
	}
	registry.Register(tool)

	if len(registry.List()) != 1 {
		t.Errorf("expected 1 tool, got %d", len(registry.List()))
	}

	retrieved, ok := registry.Get("get_today_summary")
	if !ok || retrieved != tool {
		t.Errorf("failed to retrieve registered tool")
	}
}

func TestSaveTransactionTool_Validation(t *testing.T) {
	tool := &SaveTransactionTool{}

	// Valid payload
	parsed, err := tool.Validate(`{"type":"expense","amount":15000,"category":"food","description":"jajan kopi"}`)
	if err != nil {
		t.Fatalf("expected no validation error, got: %v", err)
	}
	args := parsed.(*SaveTransactionArgs)
	if args.Type != "expense" || args.Amount != 15000 || args.Category != "food" || args.Description != "jajan kopi" {
		t.Errorf("parsed arguments mismatch: %+v", args)
	}

	// Invalid type
	_, err = tool.Validate(`{"type":"invalid","amount":15000,"category":"food","description":"jajan"}`)
	if err == nil {
		t.Error("expected error for invalid type")
	}

	// Negative amount
	_, err = tool.Validate(`{"type":"expense","amount":-5000,"category":"food","description":"jajan"}`)
	if err == nil {
		t.Error("expected error for negative amount")
	}

	// Invalid category
	_, err = tool.Validate(`{"type":"expense","amount":5000,"category":"luxury","description":"jajan"}`)
	if err == nil {
		t.Error("expected error for invalid category")
	}

	// Empty description
	_, err = tool.Validate(`{"type":"expense","amount":5000,"category":"food","description":""}`)
	if err == nil {
		t.Error("expected error for empty description")
	}

	// Invalid JSON syntax
	_, err = tool.Validate(`{invalid}`)
	if err == nil {
		t.Error("expected error for invalid JSON syntax")
	}
}

func TestGetTransactionsTool_Validation(t *testing.T) {
	tool := &GetTransactionsTool{}

	// Valid empty payload (should use defaults)
	parsed, err := tool.Validate(`{}`)
	if err != nil {
		t.Fatalf("expected no validation error for empty payload, got: %v", err)
	}
	args := parsed.(*GetTransactionsArgs)
	if args.Limit != 10 || args.Type != "" {
		t.Errorf("expected default limit 10, got %d and type %q", args.Limit, args.Type)
	}

	// Valid payload with limit and type
	parsed, err = tool.Validate(`{"limit":5,"type":"income"}`)
	if err != nil {
		t.Fatalf("expected no validation error, got: %v", err)
	}
	args = parsed.(*GetTransactionsArgs)
	if args.Limit != 5 || args.Type != "income" {
		t.Errorf("expected limit 5 and type income, got limit %d and type %q", args.Limit, args.Type)
	}

	// Invalid type filter
	_, err = tool.Validate(`{"limit":5,"type":"borrow"}`)
	if err == nil {
		t.Error("expected error for invalid type filter")
	}

	// Negative limit
	_, err = tool.Validate(`{"limit":-5}`)
	if err == nil {
		t.Error("expected error for negative limit")
	}
}

func TestRegistry_Dispatch(t *testing.T) {
	registry := NewRegistry()

	handlerCalled := false
	tool := &SaveTransactionTool{
		Handler: func(ctx context.Context, telegramID int64, args *SaveTransactionArgs) (interface{}, error) {
			handlerCalled = true
			if telegramID != 12345 {
				return nil, errors.New("incorrect telegram ID")
			}
			return "saved success", nil
		},
	}
	registry.Register(tool)

	// Successful dispatch
	resp, err := registry.Dispatch(
		context.Background(),
		12345,
		"save_transaction",
		`{"type":"expense","amount":25000,"category":"food","description":"bakso"}`,
	)
	if err != nil {
		t.Fatalf("expected successful dispatch, got error: %v", err)
	}
	if resp != "saved success" {
		t.Errorf("expected response 'saved success', got %q", resp)
	}
	if !handlerCalled {
		t.Error("handler was not called")
	}

	// Dispatch tool that doesn't exist
	_, err = registry.Dispatch(context.Background(), 12345, "invalid_tool", `{}`)
	if err == nil {
		t.Error("expected error for non-existent tool dispatch")
	}

	// Dispatch tool with validation error
	_, err = registry.Dispatch(context.Background(), 12345, "save_transaction", `{"type":"invalid"}`)
	if err == nil {
		t.Error("expected validation error during dispatch")
	}
}

func TestSetMonthlyBudgetTool_Validation(t *testing.T) {
	tool := &SetMonthlyBudgetTool{}

	// Valid payload
	parsed, err := tool.Validate(`{"amount":5000000}`)
	if err != nil {
		t.Fatalf("expected no validation error, got: %v", err)
	}
	args := parsed.(*SetMonthlyBudgetArgs)
	if args.Amount != 5000000 {
		t.Errorf("parsed amount mismatch: %d", args.Amount)
	}

	// Negative amount
	_, err = tool.Validate(`{"amount":-1000}`)
	if err == nil {
		t.Error("expected error for negative budget amount")
	}
}

func TestSetCategoryBudgetTool_Validation(t *testing.T) {
	tool := &SetCategoryBudgetTool{}

	// Valid payload
	parsed, err := tool.Validate(`{"category":"food","amount":500000}`)
	if err != nil {
		t.Fatalf("expected no validation error, got: %v", err)
	}
	args := parsed.(*SetCategoryBudgetArgs)
	if args.Category != "food" || args.Amount != 500000 {
		t.Errorf("parsed args mismatch: %+v", args)
	}

	// Invalid category
	_, err = tool.Validate(`{"category":"luxury","amount":500000}`)
	if err == nil {
		t.Error("expected error for invalid category")
	}

	// Negative amount
	_, err = tool.Validate(`{"category":"food","amount":-1}`)
	if err == nil {
		t.Error("expected error for negative category amount")
	}
}

func TestDeleteTransactionTool_Validation(t *testing.T) {
	tool := &DeleteTransactionTool{}

	// Valid payload with id
	parsed, err := tool.Validate(`{"id":"d3b07384-d113-4ec6-a558-71311b587cf5"}`)
	if err != nil {
		t.Fatalf("expected no validation error, got: %v", err)
	}
	args := parsed.(*DeleteTransactionArgs)
	if args.ID != "d3b07384-d113-4ec6-a558-71311b587cf5" || args.Last {
		t.Errorf("parsed args mismatch: %+v", args)
	}

	// Valid payload with last=true
	parsed, err = tool.Validate(`{"last":true}`)
	if err != nil {
		t.Fatalf("expected no validation error, got: %v", err)
	}
	args = parsed.(*DeleteTransactionArgs)
	if args.ID != "" || !args.Last {
		t.Errorf("parsed args mismatch: %+v", args)
	}
}
