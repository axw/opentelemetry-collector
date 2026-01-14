// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telemetry // import "go.opentelemetry.io/collector/telemetry"

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type loggerKey struct{}

// LoggerFromContext retrieves a zap.Logger from the context, and a
// boolean indicating whether it was found.
//
// Most callers should prefer component.TelemetrySettings.ContextLogger,
// which provides a fallback to the TelemetrySettings.Logger field in
// case there is no logger in the context.
func LoggerFromContext(ctx context.Context) (*zap.Logger, bool) {
	logger, ok := ctx.Value(loggerKey{}).(*zap.Logger)
	return logger, ok
}

// ContextWithLogger returns a copy of the given context with the
// provided zap.Logger.
func ContextWithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey{}, logger)
}

// TraceContextFields extracts trace information from the context
// and returns zap.Fields for logging.
func TraceContextFields(ctx context.Context) []zap.Field {
	tracer := trace.SpanFromContext(ctx).SpanContext()
	if !tracer.IsValid() {
		return nil
	}
	return []zap.Field{
		zap.String("trace_id", tracer.TraceID().String()),
		zap.String("span_id", tracer.SpanID().String()),
	}
}
