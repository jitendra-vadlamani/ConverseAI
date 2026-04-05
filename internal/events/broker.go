package events

import (
	"sync"
	"ai-chat/internal/model"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type EventBroker interface {
	Publish(event model.ConversationEvent)
	Subscribe(conversationID primitive.ObjectID) <-chan model.ConversationEvent
	Unsubscribe(conversationID primitive.ObjectID, ch <-chan model.ConversationEvent)
}

type eventBroker struct {
	mu          sync.RWMutex
	subscribers map[primitive.ObjectID][]chan model.ConversationEvent
}

func NewEventBroker() EventBroker {
	return &eventBroker{
		subscribers: make(map[primitive.ObjectID][]chan model.ConversationEvent),
	}
}

func (b *eventBroker) Publish(event model.ConversationEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if chans, ok := b.subscribers[event.ConversationID]; ok {
		for _, ch := range chans {
			select {
			case ch <- event:
			default:
			}
		}
	}
}

func (b *eventBroker) Subscribe(conversationID primitive.ObjectID) <-chan model.ConversationEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan model.ConversationEvent, 100)
	b.subscribers[conversationID] = append(b.subscribers[conversationID], ch)
	return ch
}

func (b *eventBroker) Unsubscribe(conversationID primitive.ObjectID, ch <-chan model.ConversationEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if chans, ok := b.subscribers[conversationID]; ok {
		for i, c := range chans {
			if c == ch {
				close(c) // Close the channel so consumers detect the disconnect
				b.subscribers[conversationID] = append(chans[:i], chans[i+1:]...)
				break
			}
		}
		if len(b.subscribers[conversationID]) == 0 {
			delete(b.subscribers, conversationID)
		}
	}
}
