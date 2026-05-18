package log

import (
	"fmt"
	"strings"
)

// Fields holds structured key-value pairs for logging.
type Fields map[string]interface{}

type fieldKey struct{}

// Entry wraps a Logger with accumulated structured fields.
type Entry struct {
	fields Fields
}

// WithField returns an Entry with a single key-value pair.
func WithField(key string, value interface{}) *Entry {
	return &Entry{fields: Fields{key: value}}
}

// WithFields returns an Entry with multiple key-value pairs.
func WithFields(fields Fields) *Entry {
	f := make(Fields, len(fields))
	for k, v := range fields {
		f[k] = v
	}
	return &Entry{fields: f}
}

// WithField adds a key-value pair to the Entry and returns it.
func (e *Entry) WithField(key string, value interface{}) *Entry {
	f := make(Fields, len(e.fields)+1)
	for k, v := range e.fields {
		f[k] = v
	}
	f[key] = value
	return &Entry{fields: f}
}

// WithFields merges fields into the Entry and returns it.
func (e *Entry) WithFields(fields Fields) *Entry {
	f := make(Fields, len(e.fields)+len(fields))
	for k, v := range e.fields {
		f[k] = v
	}
	for k, v := range fields {
		f[k] = v
	}
	return &Entry{fields: f}
}

// formatFields returns "key=value key=value" suffix for text output.
func (e *Entry) formatFields() string {
	if len(e.fields) == 0 {
		return ""
	}
	var sb strings.Builder
	for k, v := range e.fields {
		sb.WriteByte(' ')
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(fmt.Sprintf("%v", v))
	}
	return sb.String()
}

// Fields returns the underlying Fields map.
func (e *Entry) Fields() Fields {
	return e.fields
}

func (e *Entry) Fatal(v ...interface{}) {
	args := append([]interface{}(nil), v...)
	args = append(args, e.formatFields())
	logger.Fatal(args...)
}

func (e *Entry) Fatalf(format string, v ...interface{}) {
	logger.Fatalf(format+e.formatFields(), v...)
}

func (e *Entry) Error(v ...interface{}) {
	args := append([]interface{}(nil), v...)
	args = append(args, e.formatFields())
	logger.Error(args...)
}

func (e *Entry) Errorf(format string, v ...interface{}) {
	logger.Errorf(format+e.formatFields(), v...)
}

func (e *Entry) Warn(v ...interface{}) {
	args := append([]interface{}(nil), v...)
	args = append(args, e.formatFields())
	logger.Warn(args...)
}

func (e *Entry) Warnf(format string, v ...interface{}) {
	logger.Warnf(format+e.formatFields(), v...)
}

func (e *Entry) Info(v ...interface{}) {
	args := append([]interface{}(nil), v...)
	args = append(args, e.formatFields())
	logger.Info(args...)
}

func (e *Entry) Infof(format string, v ...interface{}) {
	logger.Infof(format+e.formatFields(), v...)
}

func (e *Entry) Debug(v ...interface{}) {
	args := append([]interface{}(nil), v...)
	args = append(args, e.formatFields())
	logger.Debug(args...)
}

func (e *Entry) Debugf(format string, v ...interface{}) {
	logger.Debugf(format+e.formatFields(), v...)
}

func (e *Entry) Trace(v ...interface{}) {
	args := append([]interface{}(nil), v...)
	args = append(args, e.formatFields())
	logger.Trace(args...)
}

func (e *Entry) Tracef(format string, v ...interface{}) {
	logger.Tracef(format+e.formatFields(), v...)
}
