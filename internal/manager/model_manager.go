package manager

import (
	"context"
	"fmt"
	"sync"

	"ai-chat/internal/ollama"
)

type ModelManager interface {
	PrepareModel(ctx context.Context, modelName string) error
	GetActiveModel() string
}

type modelManager struct {
	client          ollama.Client
	activeModelName string
	mu              sync.Mutex
}

func NewModelManager(client ollama.Client) ModelManager {
	return &modelManager{
		client: client,
	}
}

func (m *modelManager) PrepareModel(ctx context.Context, modelName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeModelName == modelName {
		return nil
	}

	if m.activeModelName != "" {
		fmt.Printf("[ModelManager] Unloading previous model '%s' from VRAM\n", m.activeModelName)
		if err := m.client.Unload(ctx, m.activeModelName); err != nil {
			fmt.Printf("[ModelManager] Warning: Failed to unload model %s: %v\n", m.activeModelName, err)
		}
	}

	m.activeModelName = modelName
	return nil
}

func (m *modelManager) GetActiveModel() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeModelName
}
