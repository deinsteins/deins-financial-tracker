package services

import (
	"context"
	"finance-bot/bot/llm"
)

type OrchestrationService interface {
	ParseIntent(ctx context.Context, history []llm.Message, text string) (*llm.ToolIntent, error)
	Registry() *llm.Registry
	Dispatch(ctx context.Context, telegramID int64, name string, argsRaw string) (interface{}, error)
}

type orchestrationService struct {
	client   llm.HermesClient
	registry *llm.Registry
}

func NewOrchestrationService(client llm.HermesClient, registry *llm.Registry, finance FinanceService) OrchestrationService {
	svc := &orchestrationService{
		client:   client,
		registry: registry,
	}
	svc.registerTools(finance)
	return svc
}

func (s *orchestrationService) registerTools(finance FinanceService) {
	s.registry.Register(&llm.SaveTransactionTool{
		Handler: func(ctx context.Context, telegramID int64, args *llm.SaveTransactionArgs) (interface{}, error) {
			walletName, _ := ctx.Value("wallet").(string)
			return finance.AddTransaction(telegramID, args.Type, args.Category, args.Amount, args.Description, walletName)
		},
	})
	s.registry.Register(&llm.GetTodaySummaryTool{
		Handler: func(ctx context.Context, telegramID int64) (interface{}, error) {
			return finance.GetTodaySummary(telegramID)
		},
	})
	s.registry.Register(&llm.GetMonthSummaryTool{
		Handler: func(ctx context.Context, telegramID int64) (interface{}, error) {
			return finance.GetMonthSummary(telegramID)
		},
	})
	s.registry.Register(&llm.GetTransactionsTool{
		Handler: func(ctx context.Context, telegramID int64, args *llm.GetTransactionsArgs) (interface{}, error) {
			return finance.GetTransactions(telegramID, args.Limit, args.Type)
		},
	})
	s.registry.Register(&llm.AnalyzeSpendingTool{
		Handler: func(ctx context.Context, telegramID int64) (interface{}, error) {
			return finance.GenerateAIAnalysis(telegramID)
		},
	})
	s.registry.Register(&llm.SetMonthlyBudgetTool{
		Handler: func(ctx context.Context, telegramID int64, args *llm.SetMonthlyBudgetArgs) (interface{}, error) {
			return finance.SetMonthlyBudget(telegramID, args.Amount)
		},
	})
	s.registry.Register(&llm.SetCategoryBudgetTool{
		Handler: func(ctx context.Context, telegramID int64, args *llm.SetCategoryBudgetArgs) (interface{}, error) {
			return finance.SetCategoryBudget(telegramID, args.Category, args.Amount)
		},
	})
}

func (s *orchestrationService) ParseIntent(ctx context.Context, history []llm.Message, text string) (*llm.ToolIntent, error) {
	return s.client.GetIntent(ctx, history, text)
}

func (s *orchestrationService) Registry() *llm.Registry {
	return s.registry
}

func (s *orchestrationService) Dispatch(ctx context.Context, telegramID int64, name string, argsRaw string) (interface{}, error) {
	return s.registry.Dispatch(ctx, telegramID, name, argsRaw)
}
