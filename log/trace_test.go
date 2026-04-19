package log

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithTrace_MintsUniqueIDs(t *testing.T) {
	ctx1, id1 := WithTrace(context.Background())
	ctx2, id2 := WithTrace(context.Background())

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Equal(t, id1, TraceID(ctx1))
	assert.Equal(t, id2, TraceID(ctx2))

	// Format is "t<digits>" or "t<digits>-<hex>".
	assert.Regexp(t, regexp.MustCompile(`^t\d+(-[0-9a-f]{4})?$`), id1)
}

func TestWithTrace_ReusesExistingID(t *testing.T) {
	ctx1, id1 := WithTrace(context.Background())
	ctx2, id2 := WithTrace(ctx1)

	assert.Equal(t, id1, id2, "re-wrapping a traced ctx should not mint a fresh ID")
	assert.Same(t, ctx1, ctx2, "returned ctx should be the original")
}

func TestTraceID_EmptyOnUntracedOrNilCtx(t *testing.T) {
	// Explicit nil-context test: the indirection silences staticcheck's
	// SA1012 rule, which is generally right — but here we want to verify
	// TraceID's documented nil safety.
	var nilCtx context.Context
	assert.Equal(t, "", TraceID(nilCtx))
	assert.Equal(t, "", TraceID(context.Background()))
}

func TestLoggerFrom_PrefersCtxLogger(t *testing.T) {
	t.Setenv(EnvLogFormat, "json")
	origStructured := Structured
	t.Cleanup(func() { Structured = origStructured })

	var baseBuf, ctxBuf bytes.Buffer
	Structured = newStructured(&baseBuf, false)
	ctxLogger := newStructured(&ctxBuf, false).With("subsystem", "ctx")

	ctx := WithLogger(context.Background(), ctxLogger)
	LoggerFrom(ctx).Info("hit")

	assert.Empty(t, baseBuf.String(), "base logger must not receive the record")
	assert.Contains(t, ctxBuf.String(), `"msg":"hit"`)
	assert.Contains(t, ctxBuf.String(), `"subsystem":"ctx"`)
}

func TestLoggerFrom_FallsBackToStructured(t *testing.T) {
	t.Setenv(EnvLogFormat, "")
	origStructured := Structured
	origLevel := levelVar.Level()
	t.Cleanup(func() {
		Structured = origStructured
		levelVar.Set(origLevel)
	})

	var buf bytes.Buffer
	levelVar.Set(slog.LevelInfo)
	Structured = newStructured(&buf, false)

	LoggerFrom(context.Background()).Info("fallback")
	assert.Contains(t, buf.String(), "fallback")
}

func TestLoggerFrom_NoOpWhenUninitialized(t *testing.T) {
	origStructured := Structured
	Structured = nil
	t.Cleanup(func() { Structured = origStructured })

	logger := LoggerFrom(context.Background())
	assert.NotNil(t, logger)
	assert.NotPanics(t, func() { logger.Info("no-op ok") })
}
