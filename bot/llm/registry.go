package llm

import (
	"context"
	"fmt"
)

// Tool defines the interface that all Hermes tools must implement.
type Tool interface {
	Name() string
	Description() string
	Parameters() Parameters
	Validate(argsRaw string) (interface{}, error)
	Execute(ctx context.Context, telegramID int64, argsRaw string) (interface{}, error)
}

// Registry manages tool registration, retrieval, and dispatching.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry.
type NewRegistryFunc func() *Registry

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tools.
func (r *Registry) List() []Tool {
	list := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		list = append(list, t)
	}
	return list
}

// Dispatch validates the payload and executes the tool.
func (r *Registry) Dispatch(ctx context.Context, telegramID int64, name string, argsRaw string) (interface{}, error) {
	t, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %q not found in registry", name)
	}

	// 1. Validate payload
	_, err := t.Validate(argsRaw)
	if err != nil {
		return nil, fmt.Errorf("validation error for tool %q: %w", name, err)
	}

	// 2. Execute tool
	res, err := t.Execute(ctx, telegramID, argsRaw)
	if err != nil {
		return nil, fmt.Errorf("execution error for tool %q: %w", name, err)
	}

	return res, nil
}
