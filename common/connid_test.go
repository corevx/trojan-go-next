package common

import (
	"context"
	"sync"
	"testing"
)

func TestNewConnID_Increments(t *testing.T) {
	id1 := NewConnID()
	id2 := NewConnID()
	if id1 >= id2 {
		t.Errorf("expected id1 < id2, got %d >= %d", id1, id2)
	}
}

func TestContextWithConnID(t *testing.T) {
	id := NewConnID()
	ctx := ContextWithConnID(context.Background(), id)
	got := ConnIDFromContext(ctx)
	if got != id {
		t.Errorf("expected %d, got %d", id, got)
	}
}

func TestConnIDFromContext_Missing(t *testing.T) {
	got := ConnIDFromContext(context.Background())
	if got != 0 {
		t.Errorf("expected 0 for missing key, got %d", got)
	}
}

func TestNewConnID_ConcurrentSafety(t *testing.T) {
	const goroutines = 100
	ids := make([]ConnID, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ids[idx] = NewConnID()
		}(i)
	}
	wg.Wait()

	seen := make(map[ConnID]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate conn ID: %d", id)
		}
		seen[id] = true
	}
}
