package log

import "context"

// ContextWithFields stores structured fields in the context.
func ContextWithFields(ctx context.Context, fields Fields) context.Context {
	return context.WithValue(ctx, fieldKey{}, fields)
}

// FromContext extracts fields from the context and returns an Entry.
// Returns a plain Entry (no fields) if none found.
func FromContext(ctx context.Context) *Entry {
	if ctx == nil {
		return &Entry{fields: Fields{}}
	}
	if f, ok := ctx.Value(fieldKey{}).(Fields); ok {
		return WithFields(f)
	}
	return &Entry{fields: Fields{}}
}
