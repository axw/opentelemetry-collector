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

type Providers struct {
	shutdown       func(context.Context) error
	resource       pcommon.Resource
	logger         *zap.Logger
	loggerProvider log.LoggerProvider
	meterProvider  metric.MeterProvider
	tracerProvider trace.TracerProvider
}

func NewProviders(opts ...Option) *Providers {
	tel := &Providers{
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
	apply(*Providers)
}

type optionFunc func(*Providers)

func (f optionFunc) apply(p *Providers) {
	f(p)
}

func WithShutdown(shutdown func(context.Context) error) Option {
	return optionFunc(func(p *Providers) {
		p.shutdown = shutdown
	})
}

func WithResource(res pcommon.Resource) Option {
	return optionFunc(func(p *Providers) {
		p.resource = res
	})
}

func WithLogger(logger *zap.Logger) Option {
	return optionFunc(func(p *Providers) {
		p.logger = logger
	})
}

func WithLoggerProvider(loggerProvider log.LoggerProvider) Option {
	return optionFunc(func(p *Providers) {
		p.loggerProvider = loggerProvider
	})
}

func WithMeterProvider(meterProvider metric.MeterProvider) Option {
	return optionFunc(func(p *Providers) {
		p.meterProvider = meterProvider
	})
}

func WithTracerProvider(tracerProvider trace.TracerProvider) Option {
	return optionFunc(func(p *Providers) {
		p.tracerProvider = tracerProvider
	})
}

func (p *Providers) Resource() pcommon.Resource {
	return p.resource
}

func (p *Providers) Logger() *zap.Logger {
	return p.logger
}

func (p *Providers) LoggerProvider() log.LoggerProvider {
	return p.loggerProvider
}

func (p *Providers) MeterProvider() metric.MeterProvider {
	return p.meterProvider
}

func (p *Providers) TracerProvider() trace.TracerProvider {
	return p.tracerProvider
}

func (p *Providers) Shutdown(ctx context.Context) error {
	return p.shutdown(ctx)
}
