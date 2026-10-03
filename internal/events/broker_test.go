package events

import (
	"context"
	"testing"
)

func TestMemoryBrokerFanOutAndUnsubscribe(t *testing.T) {
	b := NewMemoryBroker()
	ctx := context.Background()
	a, cancelA := b.Subscribe(ctx, "t")
	c, cancelC := b.Subscribe(ctx, "t")
	b.Publish(ctx, "t", []byte("1"))
	b.Publish(ctx, "other", []byte("x"))
	if string(<-a) != "1" || string(<-c) != "1" {
		t.Fatal("both subscribers get the message")
	}
	cancelA()
	cancelA() // idempotent
	if _, ok := <-a; ok {
		t.Fatal("channel closed after cancel")
	}
	b.Publish(ctx, "t", []byte("2"))
	if string(<-c) != "2" {
		t.Fatal("remaining subscriber still receives")
	}
	cancelC()
}

func TestMemoryBrokerDropsLaggingSubscriber(t *testing.T) {
	b := NewMemoryBroker()
	ctx := context.Background()
	ch, cancel := b.Subscribe(ctx, "t")
	defer cancel()
	for i := 0; i < subscriberBuffer+1; i++ {
		b.Publish(ctx, "t", []byte("x"))
	}
	n := 0
	for range ch {
		n++
	}
	if n != subscriberBuffer {
		t.Fatalf("lagging subscriber should be closed after its buffer, got %d", n)
	}
}
