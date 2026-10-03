// Package manager schedules access to the GPU across concurrent requests.
package manager

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"ai-chat/internal/metrics"
	"ai-chat/internal/ollama"
)

// ModelManager hands out leases on a model. In single-model mode only one
// model may be in use at a time: a request for a different model waits until
// every lease on the current one is released, then the old model is unloaded.
// This replaces the old global "active model" that let one request unload the
// model another request was still streaming from.
type ModelManager interface {
	Acquire(ctx context.Context, modelName string) (release func(), err error)
}

type modelManager struct {
	client     ollama.Client
	singleMode bool

	mu      sync.Mutex
	active  string
	inUse   int
	waiting int
	changed chan struct{} // closed and replaced whenever inUse drops to 0
}

func NewModelManager(client ollama.Client, singleMode bool) ModelManager {
	return &modelManager{client: client, singleMode: singleMode, changed: make(chan struct{})}
}

func (m *modelManager) Acquire(ctx context.Context, modelName string) (func(), error) {
	if !m.singleMode {
		return func() {}, nil
	}
	start := time.Now()
	queued := false
	defer func() {
		if queued {
			metrics.ModelQueueDepth.Dec()
		}
		metrics.ModelWaitSeconds.Observe(time.Since(start).Seconds())
	}()

	for {
		m.mu.Lock()
		// Same model, and nobody is queued for a different one: share it.
		// Otherwise wait for the GPU to drain so a queued model isn't starved.
		if m.active == modelName && (m.waiting == 0 || m.inUse == 0) || m.inUse == 0 {
			if m.active != modelName && m.active != "" {
				prev := m.active
				// Unload with a context of its own: if this request is cancelled
				// the old model must still be evicted.
				uctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := m.client.Unload(uctx, prev); err != nil {
					slog.Warn("unload model failed", "model", prev, "err", err)
				}
				cancel()
			}
			m.active = modelName
			m.inUse++
			if queued {
				m.waiting--
			}
			m.mu.Unlock()
			var once sync.Once
			return func() { once.Do(m.release) }, nil
		}
		if !queued {
			queued = true
			m.waiting++
			metrics.ModelQueueDepth.Inc()
		}
		wait := m.changed
		m.mu.Unlock()

		select {
		case <-wait:
		case <-ctx.Done():
			m.mu.Lock()
			m.waiting--
			m.mu.Unlock()
			return nil, ctx.Err()
		}
	}
}

func (m *modelManager) release() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inUse--
	if m.inUse == 0 {
		close(m.changed)
		m.changed = make(chan struct{})
	}
}
