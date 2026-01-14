// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package component // import "go.opentelemetry.io/collector/component"

import (
	"context"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

// TelemetrySettings provides components with APIs to report telemetry.
type TelemetrySettings struct {
	// Logger that the factory can use during creation and can pass to the created
	// component to be used later as well.
	Logger *zap.Logger

	// TracerProvider that the factory can pass to other instrumented third-party libraries.
	//
	// The service may wrap this provider for attribute injection. The wrapper may implement an
	// additional `Unwrap() trace.TracerProvider` method to grant access to the underlying SDK.
	TracerProvider trace.TracerProvider

	// MeterProvider that the factory can pass to other instrumented third-party libraries.
	MeterProvider metric.MeterProvider

	// Resource contains the resource attributes for the collector's telemetry.
	Resource pcommon.Resource

	// prevent unkeyed literal initialization
	_ struct{}
}

// ContextLogger returns a logger from the context, or the TelemetrySettings's
// logger if none is found. This allows components to use a logger that has
// been decorated with context-specific fields, such as trace or request IDs.
func (s *TelemetrySettings) ContextLogger(ctx context.Context) *zap.Logger {
	if logger, ok := LoggerFromContext(ctx); ok {
		return logger
	}
	return s.Logger
}

// NOTE: code below probably belongs elsewhere, e.g. in a telemetry package

type loggerKey struct{}

func LoggerFromContext(ctx context.Context) (*zap.Logger, bool) {
	logger, ok := ctx.Value(loggerKey{}).(*zap.Logger)
	return logger, ok
}

func ContextWithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey{}, logger)
}

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
