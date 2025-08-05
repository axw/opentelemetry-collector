// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telemetry // import "go.opentelemetry.io/collector/service/telemetry"

import (
	"context"

	config "go.opentelemetry.io/contrib/otelconf/v0.3.0"
	"go.opentelemetry.io/otel/log"
	nooplog "go.opentelemetry.io/otel/log/noop"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// Telemetry is an interface for instantiating internal collector
// telemetry providers.
type Telemetry interface {
	// Shutdown gracefully shuts down the telemetry components.
	Shutdown(context.Context) error

	// Resource returns a pcommon.Resource representing the collector.
	// This may be used by components in their internal telemetry.
	Resource() pcommon.Resource

	// Logger returns a zap.Logger that may be used by components to
	// log their internal operations.
	//
	// NOTE: from the perspective of the Telemetry implementation,
	// this Logger and the LoggerProvider are independent. The service
	// package will arrange for logs written to this logger to be
	// copied to the LoggerProvider.
	Logger() *zap.Logger

	// LoggerProvider returns a log.LoggerProvider that may be used
	// for components to log their internal operations.
	LoggerProvider() log.LoggerProvider

	// MeterProvider returns a metric.MeterProvider that may be used
	// by components to record metrics relating to their internal
	// operations.
	MeterProvider() metric.MeterProvider

	// TracerProvider returns a trace.TracerProvider that may be used
	// by components to trace their internal operations.
	TracerProvider() trace.TracerProvider
}

// Settings holds configuration settings for Telemetry creators.
type Settings struct {
	// BuildInfo holds build information about the collector.
	BuildInfo component.BuildInfo

	// DefaultViews is a function that returns the default metric views
	// for the collector's internal telemetry, for the given level.
	//
	// This must be used if and only if no views have been configured.
	DefaultViews func(configtelemetry.Level) []config.View

	// ZapOptions holds additional options to use when creating a Zap logger.
	ZapOptions []zap.Option
}

// Factory is a factory interface for internal telemetry.
//
// This interface cannot be directly implemented. Implementations must
// use the NewFactory to implement it.
type Factory interface {
	// CreateDefaultConfig creates the default configuration for the telemetry.
	// TODO: Should we just inherit from component.Factory?
	CreateDefaultConfig() component.Config

	// CreateProviders creates the logger, meter, and tracer providers for
	// the collector's internal telemetry.
	CreateTelemetry(context.Context, Settings, component.Config) (Telemetry, error)

	// unexportedFactoryFunc is used to prevent external implementations of Factory.
	unexportedFactoryFunc()
}

type FactoryOption interface {
	applyOption(*factory)
}

// factoryOptionFunc is an FactoryOption created through a function.
type factoryOptionFunc func(*factory)

func (f factoryOptionFunc) applyOption(o *factory) {
	f(o)
}

type factory struct {
	component.CreateDefaultConfigFunc
	createTelemetryFunc CreateTelemetryFunc
}

// NewFactory returns a Factory.
//
// If createTelemetry is nil, then the returned Factory's
// CreateTelemetry method will return a Telemetry with
// noop telemetry providers.
func NewFactory(
	createDefaultConfig component.CreateDefaultConfigFunc,
	createTelemetry CreateTelemetryFunc,
	opts ...FactoryOption,
) Factory {
	f := &factory{
		CreateDefaultConfigFunc: createDefaultConfig,
		createTelemetryFunc:     createTelemetry,
	}
	for _, opt := range opts {
		opt.applyOption(f)
	}
	return f
}

// CreateTelemetryFunc is the equivalent of Factory.CreateTelemetry.
type CreateTelemetryFunc func(context.Context, Settings, component.Config) (Telemetry, error)

func (*factory) unexportedFactoryFunc() {}

func (f *factory) CreateTelemetry(ctx context.Context, settings Settings, cfg component.Config) (Telemetry, error) {
	if f.createTelemetryFunc == nil {
		return nopTelemetry{}, nil
	}
	return f.createTelemetryFunc(ctx, settings, cfg)
}

type nopTelemetry struct{}

func (nopTelemetry) Shutdown(context.Context) error {
	return nil
}

func (nopTelemetry) Resource() pcommon.Resource {
	return pcommon.NewResource()
}

func (nopTelemetry) Logger() *zap.Logger {
	return zap.NewNop()
}

func (nopTelemetry) LoggerProvider() log.LoggerProvider {
	return nooplog.NewLoggerProvider()
}

func (nopTelemetry) MeterProvider() metric.MeterProvider {
	return noopmetric.NewMeterProvider()
}

func (nopTelemetry) TracerProvider() trace.TracerProvider {
	return nooptrace.NewTracerProvider()
}
