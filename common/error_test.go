package common

import (
	"errors"
	"io"
	"testing"
)

func TestErrorUnwrapChain(t *testing.T) {
	inner := errors.New("root cause")
	middle := NewError("middle").Base(inner)
	outer := NewError("outer").Base(middle)

	if outer.Error() != "outer | middle | root cause" {
		t.Errorf("Error() format changed: %q", outer.Error())
	}
	if errors.Unwrap(outer) != middle {
		t.Errorf("Unwrap(outer) should return middle, got %v", errors.Unwrap(outer))
	}
	if !errors.Is(outer, inner) {
		t.Error("errors.Is should see through the common.Error chain")
	}
}

func TestErrorUnwrapNilCause(t *testing.T) {
	err := NewError("no base")
	if errors.Unwrap(err) != nil {
		t.Errorf("Unwrap of a baseless error should be nil, got %v", errors.Unwrap(err))
	}
}

func TestErrorBaseNilIsNoop(t *testing.T) {
	err := NewError("x").Base(nil)
	if err.Error() != "x" {
		t.Errorf("Base(nil) should not alter the message, got %q", err.Error())
	}
	if errors.Unwrap(err) != nil {
		t.Errorf("Base(nil) should not set a cause, got %v", errors.Unwrap(err))
	}
}

func TestErrorIsSentinelThroughProductionChain(t *testing.T) {
	// mirrors the production UDP relay chain: metadata wraps io.EOF,
	// the trojan packet layer wraps that again
	metadataErr := NewError("unable to read ATYP").Base(io.EOF)
	packetErr := NewError("failed to parse udp packet addr").Base(metadataErr)
	if !errors.Is(packetErr, io.EOF) {
		t.Error("two-level common.Error chain should still resolve io.EOF")
	}
}
