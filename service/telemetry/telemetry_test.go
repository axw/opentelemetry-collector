// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telemetry // import "go.opentelemetry.io/collector/service/telemetry"

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	nooplog "go.opentelemetry.io/otel/log/noop"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

func TestNewFactory(t *testing.T) {
	type contextKey struct{}
	var config component.Config = new(struct{})
	var settings Settings = Settings{BuildInfo: component.NewDefaultBuildInfo()}
	ctx := context.WithValue(context.Background(), contextKey{}, 123)

	var telemetry struct {
		Telemetry
	}
	createTelemetryFunc := func(ctx context.Context, set Settings, cfg component.Config) (Telemetry, error) {
		assert.Equal(t, 123, ctx.Value(contextKey{}))
		assert.Equal(t, settings, set)
		assert.Equal(t, config, cfg)
		return &telemetry, errors.New("not implemented")
	}
	factory := NewFactory(func() component.Config { return config }, createTelemetryFunc)
	require.NotNil(t, factory)

	assert.Equal(t, config, factory.CreateDefaultConfig())
	tel, err := factory.CreateTelemetry(ctx, settings, config)
	assert.Equal(t, &telemetry, tel)
	assert.EqualError(t, err, "not implemented")
}

func TestNewFactory_Nop(t *testing.T) {
	// If the createTelemetryFunc is nil, the factory will return a Telemetry
	// implementation with no-op providers.
	factory := NewFactory(nil, nil)
	require.NotNil(t, factory)

	tel, err := factory.CreateTelemetry(context.Background(), Settings{}, nil)
	require.NoError(t, err)
	require.NotNil(t, tel)

	assert.Equal(t, pcommon.NewResource(), tel.Resource())
	assert.Equal(t, zap.NewNop(), tel.Logger())
	assert.Equal(t, nooplog.NewLoggerProvider(), tel.LoggerProvider())
	assert.Equal(t, noopmetric.NewMeterProvider(), tel.MeterProvider())
	assert.Equal(t, nooptrace.NewTracerProvider(), tel.TracerProvider())
	assert.NoError(t, tel.Shutdown(context.Background()))
}

func TestNewFactory_Options(t *testing.T) {
	var called []string
	factory := NewFactory(nil, nil, factoryOptionFunc(func(*factory) {
		called = append(called, "option1")
	}), factoryOptionFunc(func(*factory) {
		called = append(called, "option2")
	}))
	require.NotNil(t, factory)
	assert.Equal(t, []string{"option1", "option2"}, called)
}
