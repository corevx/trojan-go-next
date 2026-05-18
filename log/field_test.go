package log

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

type bufWriter struct {
	buf bytes.Buffer
}

func (w *bufWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *bufWriter) String() string               { return w.buf.String() }

func TestWithField_TextFormat(t *testing.T) {
	var bw bufWriter
	l := &testLogger{out: &bw}
	RegisterLogger(l)

	WithField("user", "abc").Info("connected")
	output := bw.String()
	if !strings.Contains(output, "[INFO]") {
		t.Errorf("expected [INFO] prefix, got: %s", output)
	}
	if !strings.Contains(output, "connected") {
		t.Errorf("expected 'connected', got: %s", output)
	}
	if !strings.Contains(output, "user=abc") {
		t.Errorf("expected 'user=abc', got: %s", output)
	}
}

func TestWithFields_TextFormat(t *testing.T) {
	var bw bufWriter
	l := &testLogger{out: &bw}
	RegisterLogger(l)

	WithFields(Fields{"user": "abc", "remote": "1.2.3.4"}).Info("test")
	output := bw.String()
	if !strings.Contains(output, "user=abc") {
		t.Errorf("expected 'user=abc', got: %s", output)
	}
	if !strings.Contains(output, "remote=1.2.3.4") {
		t.Errorf("expected 'remote=1.2.3.4', got: %s", output)
	}
}

func TestWithField_Chaining(t *testing.T) {
	var bw bufWriter
	l := &testLogger{out: &bw}
	RegisterLogger(l)

	WithField("a", 1).WithField("b", 2).Info("chain")
	output := bw.String()
	if !strings.Contains(output, "a=1") {
		t.Errorf("expected 'a=1', got: %s", output)
	}
	if !strings.Contains(output, "b=2") {
		t.Errorf("expected 'b=2', got: %s", output)
	}
}

func TestExistingLogCalls(t *testing.T) {
	var bw bufWriter
	l := &testLogger{out: &bw}
	RegisterLogger(l)

	Info("plain message")
	output := bw.String()
	if !strings.Contains(output, "plain message") {
		t.Errorf("expected 'plain message', got: %s", output)
	}
	if strings.Contains(output, "=") {
		t.Errorf("expected no fields, got: %s", output)
	}
}

func TestEntryImmutability(t *testing.T) {
	e1 := WithField("shared", "base")
	e2 := e1.WithField("extra", "only_e2")
	e3 := e1.WithField("extra", "only_e3")

	if _, ok := e2.Fields()["extra"]; !ok {
		t.Error("e2 should have 'extra' field")
	}
	if _, ok := e3.Fields()["extra"]; !ok {
		t.Error("e3 should have 'extra' field")
	}
	if e2.Fields()["extra"] == e3.Fields()["extra"] {
		t.Error("e2 and e3 should have different 'extra' values")
	}
}

// testLogger implements Logger for testing
type testLogger struct {
	out      *bufWriter
	logLevel LogLevel
}

func (l *testLogger) SetLogLevel(level LogLevel) { l.logLevel = level }
func (l *testLogger) SetOutput(io.Writer) {}

func (l *testLogger) output(prefix string, v ...interface{}) {
	var msg strings.Builder
	msg.WriteString(prefix)
	for _, val := range v {
		msg.WriteString(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(
			func() string {
				b, _ := interface{}(val).([]byte)
				if b != nil {
					return string(b)
				}
				return ""
			}(), "\n", ""), "\r", "")))
		msg.WriteByte(' ')
	}
	msg.WriteByte('\n')
	l.out.buf.WriteString(msg.String())
}

func (l *testLogger) Fatal(v ...interface{}) {
	for _, val := range v {
		l.out.buf.WriteString(func() string {
			switch v := val.(type) {
			case string:
				return v
			default:
				return ""
			}
		}())
		l.out.buf.WriteByte(' ')
	}
	l.out.buf.WriteByte('\n')
}

func (l *testLogger) Fatalf(format string, v ...interface{}) {
	l.out.buf.WriteString(format)
	l.out.buf.WriteByte('\n')
}

func (l *testLogger) Error(v ...interface{}) {
	l.out.buf.WriteString("[ERROR] ")
	for _, val := range v {
		l.out.buf.WriteString(func() string {
			switch v := val.(type) {
			case string:
				return v
			default:
				return ""
			}
		}())
		l.out.buf.WriteByte(' ')
	}
	l.out.buf.WriteByte('\n')
}

func (l *testLogger) Errorf(format string, v ...interface{}) {
	l.out.buf.WriteString("[ERROR] " + format + "\n")
}

func (l *testLogger) Warn(v ...interface{}) {
	l.out.buf.WriteString("[WARN] ")
	for _, val := range v {
		l.out.buf.WriteString(func() string {
			switch v := val.(type) {
			case string:
				return v
			default:
				return ""
			}
		}())
		l.out.buf.WriteByte(' ')
	}
	l.out.buf.WriteByte('\n')
}

func (l *testLogger) Warnf(format string, v ...interface{}) {
	l.out.buf.WriteString("[WARN] " + format + "\n")
}

func (l *testLogger) Info(v ...interface{}) {
	l.out.buf.WriteString("[INFO] ")
	for _, val := range v {
		l.out.buf.WriteString(func() string {
			switch v := val.(type) {
			case string:
				return v
			default:
				return ""
			}
		}())
		l.out.buf.WriteByte(' ')
	}
	l.out.buf.WriteByte('\n')
}

func (l *testLogger) Infof(format string, v ...interface{}) {
	l.out.buf.WriteString("[INFO] " + format + "\n")
}

func (l *testLogger) Debug(v ...interface{}) {
	l.out.buf.WriteString("[DEBUG] ")
	for _, val := range v {
		l.out.buf.WriteString(func() string {
			switch v := val.(type) {
			case string:
				return v
			default:
				return ""
			}
		}())
		l.out.buf.WriteByte(' ')
	}
	l.out.buf.WriteByte('\n')
}

func (l *testLogger) Debugf(format string, v ...interface{}) {
	l.out.buf.WriteString("[DEBUG] " + format + "\n")
}

func (l *testLogger) Trace(v ...interface{}) {
	l.out.buf.WriteString("[TRACE] ")
	for _, val := range v {
		l.out.buf.WriteString(func() string {
			switch v := val.(type) {
			case string:
				return v
			default:
				return ""
			}
		}())
		l.out.buf.WriteByte(' ')
	}
	l.out.buf.WriteByte('\n')
}

func (l *testLogger) Tracef(format string, v ...interface{}) {
	l.out.buf.WriteString("[TRACE] " + format + "\n")
}
