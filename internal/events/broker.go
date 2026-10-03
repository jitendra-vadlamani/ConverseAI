// Package events fans out live messages (run output, System Logs) to SSE
// subscribers. The in-memory broker serves a single instance; the Redis
// broker lets several API instances share streams.
package events

import (
	"context"
	"log/slog"
	"sync"

	"github.com/redis/go-redis/v9"
)

const subscriberBuffer = 1024

// Broker delivers published messages to every current subscriber of a
// topic. Delivery is best effort: a subscriber that falls more than
// subscriberBuffer messages behind has its channel closed, and must resync
// from persisted state.
type Broker interface {
	Publish(ctx context.Context, topic string, data []byte)
	// Subscribe returns a channel of messages and a function that ends the
	// subscription. The channel is closed on cancel or if the subscriber lags.
	Subscribe(ctx context.Context, topic string) (<-chan []byte, func())
	Close() error
}

func RunTopic(runID string) string           { return "run:" + runID }
func ConversationTopic(convID string) string { return "conv:" + convID }

// --- in-memory ---

type memoryBroker struct {
	mu   sync.Mutex
	subs map[string]map[chan []byte]struct{}
}

func NewMemoryBroker() Broker {
	return &memoryBroker{subs: map[string]map[chan []byte]struct{}{}}
}

func (b *memoryBroker) Publish(_ context.Context, topic string, data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[topic] {
		select {
		case ch <- data:
		default:
			// Too slow: drop the subscriber rather than block the publisher.
			delete(b.subs[topic], ch)
			close(ch)
		}
	}
}

func (b *memoryBroker) Subscribe(_ context.Context, topic string) (<-chan []byte, func()) {
	ch := make(chan []byte, subscriberBuffer)
	b.mu.Lock()
	if b.subs[topic] == nil {
		b.subs[topic] = map[chan []byte]struct{}{}
	}
	b.subs[topic][ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if _, ok := b.subs[topic][ch]; ok {
				delete(b.subs[topic], ch)
				close(ch)
			}
			if len(b.subs[topic]) == 0 {
				delete(b.subs, topic)
			}
		})
	}
}

func (b *memoryBroker) Close() error { return nil }

// --- Redis ---

type redisBroker struct {
	client *redis.Client
}

func NewRedisBroker(ctx context.Context, url string) (Broker, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &redisBroker{client: client}, nil
}

func (b *redisBroker) Publish(ctx context.Context, topic string, data []byte) {
	if err := b.client.Publish(ctx, "converseai:"+topic, data).Err(); err != nil {
		slog.Warn("redis publish failed", "topic", topic, "err", err)
	}
}

func (b *redisBroker) Subscribe(ctx context.Context, topic string) (<-chan []byte, func()) {
	ps := b.client.Subscribe(ctx, "converseai:"+topic)
	// Wait for the subscription to be live so nothing published after
	// Subscribe returns is missed.
	if _, err := ps.Receive(ctx); err != nil {
		slog.Warn("redis subscribe failed", "topic", topic, "err", err)
	}
	out := make(chan []byte, subscriberBuffer)
	in := ps.Channel(redis.WithChannelSize(subscriberBuffer))
	done := make(chan struct{})
	go func() {
		defer close(out)
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				select {
				case out <- []byte(msg.Payload):
				default:
					return // lagging subscriber
				}
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return out, func() {
		once.Do(func() {
			close(done)
			_ = ps.Close()
		})
	}
}

func (b *redisBroker) Close() error { return b.client.Close() }

// Ping reports broker health; only meaningful for Redis.
func Ping(ctx context.Context, b Broker) error {
	if rb, ok := b.(*redisBroker); ok {
		return rb.client.Ping(ctx).Err()
	}
	return nil
}
