package manager

import (
	"context"
	"testing"
	"time"

	"ai-chat/internal/testutil"
)

func TestSameModelSharesLease(t *testing.T) {
	llm := &testutil.FakeOllama{}
	m := NewModelManager(llm, true)
	r1, err := m.Acquire(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := m.Acquire(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	r1()
	r2()
	r2() // release is idempotent
	if len(llm.Unloaded) != 0 {
		t.Fatalf("nothing should be unloaded: %v", llm.Unloaded)
	}
}

func TestDifferentModelWaitsThenUnloads(t *testing.T) {
	llm := &testutil.FakeOllama{}
	m := NewModelManager(llm, true)
	release, _ := m.Acquire(context.Background(), "a")

	got := make(chan struct{})
	go func() {
		r, err := m.Acquire(context.Background(), "b")
		if err == nil {
			defer r()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("model b must wait while a is in use")
	case <-time.After(100 * time.Millisecond):
	}
	// While b waits, a new request for a must not jump the queue forever.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := m.Acquire(ctx, "a"); err == nil {
		t.Fatal("a newcomer for a should queue behind the waiting b")
	}
	release()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("b never got the GPU")
	}
	if len(llm.Unloaded) != 1 || llm.Unloaded[0] != "a" {
		t.Fatalf("a should be unloaded once, got %v", llm.Unloaded)
	}
}

func TestAcquireHonoursCancellation(t *testing.T) {
	m := NewModelManager(&testutil.FakeOllama{}, true)
	release, _ := m.Acquire(context.Background(), "a")
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := m.Acquire(ctx, "b"); err == nil {
		t.Fatal("expected context error")
	}
}

func TestMultiModelModeNeverBlocks(t *testing.T) {
	m := NewModelManager(&testutil.FakeOllama{}, false)
	r1, _ := m.Acquire(context.Background(), "a")
	defer r1()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := m.Acquire(ctx, "b"); err != nil {
		t.Fatal(err)
	}
}
