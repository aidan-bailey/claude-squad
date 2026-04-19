package log

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
)

// traceKey and loggerKey are unexported types so no external package
// can accidentally collide with our context values.
type traceKey struct{}
type loggerKey struct{}

// traceCounter is monotonic and process-local. It supplies the
// numeric prefix of every trace ID so a `sort` over grep results
// reflects ordering, which helps when reconstructing a chain of
// events from unsorted log files.
var traceCounter uint64

// WithTrace mints a new trace ID, attaches it to ctx, and returns
// both the new context and the trace ID string. Format:
// `t<monotonic>-<4 hex chars>` (e.g. `t0042-a7f3`). The random
// suffix disambiguates IDs across process restarts where the
// counter resets to zero. If ctx already carries a trace ID, it is
// reused (so a single user action produces one trace even when
// multiple layers call WithTrace).
func WithTrace(ctx context.Context) (context.Context, string) {
	if existing := TraceID(ctx); existing != "" {
		return ctx, existing
	}
	n := atomic.AddUint64(&traceCounter, 1)
	var buf [2]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// rand.Read can only fail if the OS RNG is broken, which
		// would also break TLS and every other process. Fall back
		// to the counter alone rather than panic.
		id := fmt.Sprintf("t%04d", n)
		return context.WithValue(ctx, traceKey{}, id), id
	}
	id := fmt.Sprintf("t%04d-%s", n, hex.EncodeToString(buf[:]))
	return context.WithValue(ctx, traceKey{}, id), id
}

// TraceID returns the trace ID carried on ctx, or "" if none was
// minted. Safe to call with a nil context.
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceKey{}).(string); ok {
		return v
	}
	return ""
}

// WithValueForTrace attaches an already-minted trace ID to ctx
// without touching the counter. Use when a trace survives across an
// async boundary (e.g. a scriptResumeMsg carrying the originating
// dispatch's trace) and the caller needs a ctx the engine can read
// via TraceID. Empty id yields ctx unchanged so callers do not have
// to pre-check.
func WithValueForTrace(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, traceKey{}, id)
}

// WithLogger stashes a pre-tagged *slog.Logger on ctx. Downstream
// code that wants to inherit per-request attributes (subsystem,
// instance title, trace ID) should prefer LoggerFrom(ctx) over
// calling log.For(...) again.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey{}, logger)
}

// LoggerFrom returns the *slog.Logger stashed on ctx, or the
// package-level Structured logger as a fallback. If both are nil
// (e.g. before Initialize), returns a no-op logger so callers can
// invoke methods without a nil check.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if v, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && v != nil {
			return v
		}
	}
	if Structured != nil {
		return Structured
	}
	return For("unset")
}
