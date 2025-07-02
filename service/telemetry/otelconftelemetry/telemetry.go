// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelconftelemetry

import (
	"context"
	"fmt"

	config "go.opentelemetry.io/contrib/otelconf/v0.3.0"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/multierr"
	"go.uber.org/zap"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/pdata/pcommon"

	"go.opentelemetry.io/collector/service/internal/resource"
	"go.opentelemetry.io/collector/service/telemetry"
)

type otelconfTelemetry struct {
	sdk            *config.SDK
	resource       pcommon.Resource
	logger         *zap.Logger
	loggerProvider log.LoggerProvider
	meterProvider  metric.MeterProvider
	tracerProvider trace.TracerProvider
}

func createTelemetry(
	ctx context.Context, set telemetry.Settings, componentConfig component.Config,
) (_ telemetry.Telemetry, resultErr error) {
	cfg := componentConfig.(*Config)
	res := resource.New(set.BuildInfo, cfg.Resource)
	sdk, err := newSDK(ctx, set, cfg, res)
	if err != nil {
		return nil, fmt.Errorf("failed to create SDK: %w", err)
	}
	defer func() {
		if resultErr != nil {
			resultErr = multierr.Append(resultErr, sdk.Shutdown(ctx))
		}
	}()

	pcommonRes := pcommon.NewResource()
	for _, keyValue := range res.Attributes() {
		pcommonRes.Attributes().PutStr(string(keyValue.Key), keyValue.Value.AsString())
	}

	logger, loggerProvider, err := newLogger(ctx, set, cfg, res, sdk)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger provider: %w", err)
	}

	var meterProvider metric.MeterProvider
	if cfg.Metrics.Level == configtelemetry.LevelNone {
		logger.Info("Internal metrics telemetry disabled")
		meterProvider = noopmetric.NewMeterProvider()
	} else {
		meterProvider = sdk.MeterProvider()
	}

	var tracerProvider trace.TracerProvider
	if noopTracerProvider.IsEnabled() || cfg.Traces.Level == configtelemetry.LevelNone {
		logger.Info("Internal trace telemetry disabled")
		tracerProvider = &noopNoContextTracerProvider{}
	} else {
		propagator, err := textMapPropagatorFromConfig(cfg.Traces.Propagators)
		if err != nil {
			return nil, err
		}
		otel.SetTextMapPropagator(propagator)
		tracerProvider = sdk.TracerProvider()
	}

	return &otelconfTelemetry{
		sdk:            sdk,
		resource:       pcommonRes,
		logger:         logger,
		loggerProvider: loggerProvider,
		meterProvider:  meterProvider,
		tracerProvider: tracerProvider,
	}, nil
}

func (t *otelconfTelemetry) Resource() pcommon.Resource {
	return t.resource
}

func (t *otelconfTelemetry) Logger() *zap.Logger {
	return t.logger
}

func (t *otelconfTelemetry) LoggerProvider() log.LoggerProvider {
	return t.loggerProvider
}

func (t *otelconfTelemetry) MeterProvider() metric.MeterProvider {
	return t.meterProvider
}

func (t *otelconfTelemetry) TracerProvider() trace.TracerProvider {
	return t.tracerProvider
}

func (t *otelconfTelemetry) Shutdown(ctx context.Context) error {
	if t.sdk != nil {
		err := t.sdk.Shutdown(ctx)
		t.sdk = nil
		if err != nil {
			return fmt.Errorf("failed to shutdown SDK: %w", err)
		}
	}
	return nil
}

func newSDK(ctx context.Context, set telemetry.Settings, cfg *Config, res *sdkresource.Resource) (*config.SDK, error) {
	mpConfig := &cfg.Metrics.MeterProvider
	if mpConfig.Views == nil {
		mpConfig.Views = configureViews(cfg.Metrics.Level)
	}
	if cfg.Metrics.Level == configtelemetry.LevelNone {
		mpConfig.Readers = []config.MetricReader{}
	}

	// Merge the BuildInfo-based resource attributes with the user-defined resource attributes.
	resourceAttrsMap := make(map[string]any, res.Len())
	for iter := res.Iter(); iter.Next(); {
		attr := iter.Attribute()
		resourceAttrsMap[string(attr.Key)] = attr.Value.AsString()
	}
	for k, v := range cfg.Resource {
		if v != nil {
			resourceAttrsMap[k] = *v
		} else {
			delete(resourceAttrsMap, k)
		}
	}
	resourceAttrs := make([]config.AttributeNameValue, 0, len(resourceAttrsMap))
	for k, v := range resourceAttrsMap {
		resourceAttrs = append(resourceAttrs, config.AttributeNameValue{Name: k, Value: v})
	}

	sdk, err := config.NewSDK(
		config.WithContext(ctx),
		config.WithOpenTelemetryConfiguration(
			config.OpenTelemetryConfiguration{
				LoggerProvider: &config.LoggerProvider{
					Processors: cfg.Logs.Processors,
				},
				MeterProvider:  mpConfig,
				TracerProvider: &cfg.Traces.TracerProvider,
				Resource: &config.Resource{
					SchemaUrl:  ptr(semconv.SchemaURL),
					Attributes: resourceAttrs,
				},
			},
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create SDK: %w", err)
	}
	return &sdk, nil
}
