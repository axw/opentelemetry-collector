// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telemetrytest

import (
	"context"

	"go.opentelemetry.io/otel/log"
	nooplog "go.opentelemetry.io/otel/log/noop"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

type Telemetry struct {
	shutdown       func(context.Context) error
	resource       pcommon.Resource
	logger         *zap.Logger
	loggerProvider log.LoggerProvider
	meterProvider  metric.MeterProvider
	tracerProvider trace.TracerProvider
}

func NewTelemetry(opts ...Option) *Telemetry {
	tel := &Telemetry{
		shutdown:       func(context.Context) error { return nil },
		resource:       pcommon.NewResource(),
		logger:         zap.NewNop(),
		loggerProvider: nooplog.NewLoggerProvider(),
		meterProvider:  noopmetric.NewMeterProvider(),
		tracerProvider: nooptrace.NewTracerProvider(),
	}
	for _, opt := range opts {
		opt.apply(tel)
	}
	return tel
}

type Option interface {
	apply(*Telemetry)
}

type optionFunc func(*Telemetry)

func (f optionFunc) apply(t *Telemetry) {
	f(t)
}

func WithShutdown(shutdown func(context.Context) error) Option {
	return optionFunc(func(t *Telemetry) {
		t.shutdown = shutdown
	})
}

func WithResource(res pcommon.Resource) Option {
	return optionFunc(func(t *Telemetry) {
		t.resource = res
	})
}

func WithLogger(logger *zap.Logger) Option {
	return optionFunc(func(t *Telemetry) {
		t.logger = logger
	})
}

func WithLoggerProvider(loggerProvider log.LoggerProvider) Option {
	return optionFunc(func(t *Telemetry) {
		t.loggerProvider = loggerProvider
	})
}

func WithMeterProvider(meterProvider metric.MeterProvider) Option {
	return optionFunc(func(t *Telemetry) {
		t.meterProvider = meterProvider
	})
}

func WithTracerProvider(tracerProvider trace.TracerProvider) Option {
	return optionFunc(func(t *Telemetry) {
		t.tracerProvider = tracerProvider
	})
}

func (t *Telemetry) Resource() pcommon.Resource {
	return t.resource
}

func (t *Telemetry) Logger() *zap.Logger {
	return t.logger
}

func (t *Telemetry) LoggerProvider() log.LoggerProvider {
	return t.loggerProvider
}

func (t *Telemetry) MeterProvider() metric.MeterProvider {
	return t.meterProvider
}

func (t *Telemetry) TracerProvider() trace.TracerProvider {
	return t.tracerProvider
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	return t.shutdown(ctx)
}
