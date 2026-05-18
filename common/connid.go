package common

import (
	"context"
	"sync/atomic"
)

// ConnID is a unique identifier for each connection.
type ConnID uint64

var globalConnCounter uint64

// NewConnID returns a new unique connection ID.
func NewConnID() ConnID {
	return ConnID(atomic.AddUint64(&globalConnCounter, 1))
}

type connIDKeyType struct{}

var connIDKey connIDKeyType

// ContextWithConnID stores a ConnID in the context.
func ContextWithConnID(ctx context.Context, id ConnID) context.Context {
	return context.WithValue(ctx, connIDKey, id)
}

// ConnIDFromContext extracts a ConnID from the context.
// Returns 0 if not found.
func ConnIDFromContext(ctx context.Context) ConnID {
	if id, ok := ctx.Value(connIDKey).(ConnID); ok {
		return id
	}
	return 0
}
