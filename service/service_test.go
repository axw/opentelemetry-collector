// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"go.opentelemetry.io/contrib/processors/minsev"
	"go.opentelemetry.io/contrib/zpages"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componentstatus"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/extension/zpagesextension"
	"go.opentelemetry.io/collector/internal/testutil"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pipeline"
	"go.opentelemetry.io/collector/pipeline/xpipeline"
	"go.opentelemetry.io/collector/service/extensions"
	"go.opentelemetry.io/collector/service/internal/builders"
	"go.opentelemetry.io/collector/service/pipelines"
	"go.opentelemetry.io/collector/service/telemetry"
	"go.opentelemetry.io/collector/service/telemetry/telemetrytest"
)

const (
	otelCommand = "otelcoltest"
)

var (
	nopType   = component.MustNewType("nop")
	wrongType = component.MustNewType("wrong")
)

func TestServiceGetFactory(t *testing.T) {
	set := newNopSettings()
	srv, err := New(context.Background(), set, newNopConfig())
	require.NoError(t, err)

	assert.NoError(t, srv.Start(context.Background()))
	t.Cleanup(func() {
		assert.NoError(t, srv.Shutdown(context.Background()))
	})

	assert.Nil(t, srv.host.GetFactory(component.KindReceiver, wrongType))
	assert.Equal(t, srv.host.Receivers.Factory(nopType), srv.host.GetFactory(component.KindReceiver, nopType))

	assert.Nil(t, srv.host.GetFactory(component.KindProcessor, wrongType))
	assert.Equal(t, srv.host.Processors.Factory(nopType), srv.host.GetFactory(component.KindProcessor, nopType))

	assert.Nil(t, srv.host.GetFactory(component.KindExporter, wrongType))
	assert.Equal(t, srv.host.Exporters.Factory(nopType), srv.host.GetFactory(component.KindExporter, nopType))

	assert.Nil(t, srv.host.GetFactory(component.KindConnector, wrongType))
	assert.Equal(t, srv.host.Connectors.Factory(nopType), srv.host.GetFactory(component.KindConnector, nopType))

	assert.Nil(t, srv.host.GetFactory(component.KindExtension, wrongType))
	assert.Equal(t, srv.host.Extensions.Factory(nopType), srv.host.GetFactory(component.KindExtension, nopType))

	// Try retrieve non existing component.Kind.
	assert.Nil(t, srv.host.GetFactory(component.Kind{}, nopType))
}

func TestServiceGetExtensions(t *testing.T) {
	srv, err := New(context.Background(), newNopSettings(), newNopConfig())
	require.NoError(t, err)

	assert.NoError(t, srv.Start(context.Background()))
	t.Cleanup(func() {
		assert.NoError(t, srv.Shutdown(context.Background()))
	})

	extMap := srv.host.GetExtensions()

	assert.Len(t, extMap, 1)
	assert.Contains(t, extMap, component.NewID(nopType))
}

func TestServiceGetExporters(t *testing.T) {
	srv, err := New(context.Background(), newNopSettings(), newNopConfig())
	require.NoError(t, err)

	assert.NoError(t, srv.Start(context.Background()))
	t.Cleanup(func() {
		assert.NoError(t, srv.Shutdown(context.Background()))
	})

	//nolint:staticcheck
	expMap := srv.host.GetExporters()

	v, ok := expMap[pipeline.SignalTraces]
	assert.True(t, ok)
	assert.NotNil(t, v)

	assert.Len(t, expMap, 4)
	assert.Len(t, expMap[pipeline.SignalTraces], 1)
	assert.Contains(t, expMap[pipeline.SignalTraces], component.NewID(nopType))
	assert.Len(t, expMap[pipeline.SignalMetrics], 1)
	assert.Contains(t, expMap[pipeline.SignalMetrics], component.NewID(nopType))
	assert.Len(t, expMap[pipeline.SignalLogs], 1)
	assert.Contains(t, expMap[pipeline.SignalLogs], component.NewID(nopType))
	assert.Len(t, expMap[xpipeline.SignalProfiles], 1)
	assert.Contains(t, expMap[xpipeline.SignalProfiles], component.NewID(nopType))
}

// TestServiceTelemetryCleanupOnError tests that if newService errors due to an invalid config telemetry is cleaned up
// and another service with a valid config can be started right after.
func TestServiceTelemetryCleanupOnError(t *testing.T) {
	invalidCfg := newNopConfig()
	invalidCfg.Pipelines[pipeline.NewID(pipeline.SignalTraces)].Processors[0] = component.MustNewID("invalid")
	// Create a service with an invalid config and expect an error
	_, err := New(context.Background(), newNopSettings(), invalidCfg)
	require.Error(t, err)

	// Create a service with a valid config and expect no error
	srv, err := New(context.Background(), newNopSettings(), newNopConfig())
	require.NoError(t, err)
	assert.NoError(t, srv.Shutdown(context.Background()))
}

// TestServiceTelemetry tests that the the service telemetry factory is invoked
// as expected, and that the returned telemetry providers are correctly wired up
// in the service.
func TestServiceTelemetry(t *testing.T) {
	core, observedLogs := observer.New(zapcore.InfoLevel)

	expectedResource := pcommon.NewResource()
	expectedResource.Attributes().PutStr("k", "v")

	var logsExporter inmemLogsExporter
	logsProcessor := sdklog.NewSimpleProcessor(&logsExporter)
	logsProvider := sdklog.NewLoggerProvider(sdklog.WithProcessor(
		// Only copy warning and above logs to the exporter.
		minsev.NewLogProcessor(logsProcessor, minsev.SeverityWarn),
	))

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))

	traceExporter := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(traceExporter))

	cfg := newNopConfig()
	cfg.Telemetry = fakeTelemetryConfig{}

	set := newNopSettings()
	set.BuildInfo = component.BuildInfo{Version: "test version", Command: otelCommand}
	set.TelemetryFactory = telemetry.NewFactory(
		func() component.Config { return nil },
		func(_ context.Context, set telemetry.Settings, cfg component.Config) (telemetry.Telemetry, error) {
			assert.Equal(t, fakeTelemetryConfig{}, cfg)
			return telemetrytest.NewTelemetry(
				telemetrytest.WithResource(expectedResource),
				telemetrytest.WithLogger(zap.New(core)),
				telemetrytest.WithLoggerProvider(logsProvider),
				telemetrytest.WithMeterProvider(meterProvider),
				telemetrytest.WithTracerProvider(tracerProvider),
			), nil
		},
	)

	srv, err := New(context.Background(), set, cfg)
	require.NoError(t, err)
	require.NoError(t, srv.Start(context.Background()))
	defer func() {
		assert.NoError(t, srv.Shutdown(context.Background()))
	}()

	// Check the resource is the one we expect.
	assert.Equal(t, expectedResource, srv.telemetrySettings.Resource)

	// Check the logger is the one we expect, and that the wrapping
	// keeps the level configuration intact for both the Logger and
	// LoggerProvider.
	require.NotNil(t, srv.telemetrySettings.Logger)
	assert.Equal(t, zap.InfoLevel, srv.telemetrySettings.Logger.Level())
	srv.telemetrySettings.Logger.Info("info")
	srv.telemetrySettings.Logger.Warn("warn")
	srv.telemetrySettings.Logger.Debug("debug")
	assert.Equal(t, 1, observedLogs.FilterMessage("info").Len())
	assert.Equal(t, 1, observedLogs.FilterMessage("warn").Len())
	assert.Equal(t, 0, observedLogs.FilterMessage("debug").Len())
	assert.False(t, srv.telemetry.LoggerProvider().Logger("test").Enabled(
		context.Background(),
		log.EnabledParameters{Severity: log.SeverityInfo},
	))
	assert.True(t, srv.telemetry.LoggerProvider().Logger("test").Enabled(
		context.Background(),
		log.EnabledParameters{Severity: log.SeverityWarn},
	))
	records := logsExporter.GetRecords()
	require.Len(t, records, 1)
	assert.Equal(t, "warn", records[0].Body().AsString())

	// Check the tracer provider is the one we expect.
	require.NotNil(t, srv.telemetrySettings.TracerProvider)
	_, span := srv.telemetrySettings.TracerProvider.Tracer("test").Start(context.Background(), "test-span")
	span.End()
	spans := traceExporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "test-span", spans[0].Name)

	// Check the meter provider is the one we expect, and that
	// built-in metrics are registered with the provider.
	require.NotNil(t, srv.telemetrySettings.MeterProvider)
	counter, _ := srv.telemetrySettings.MeterProvider.Meter("test").Int64Counter("custom")
	counter.Add(context.Background(), 1)
	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, metricReader.Collect(context.Background(), &resourceMetrics))
	require.Len(t, resourceMetrics.ScopeMetrics, 2)
	slices.SortFunc(resourceMetrics.ScopeMetrics, func(a, b metricdata.ScopeMetrics) int {
		return cmp.Compare(a.Scope.Name, b.Scope.Name)
	})
	assert.ElementsMatch(t, []string{
		"otelcol_process_memory_rss",
		"otelcol_process_cpu_seconds",
		"otelcol_process_runtime_total_sys_memory_bytes",
		"otelcol_process_runtime_heap_alloc_bytes",
		"otelcol_process_runtime_total_alloc_bytes",
		"otelcol_process_uptime",
	}, metricNames(resourceMetrics.ScopeMetrics[0]))
	assert.ElementsMatch(t,
		[]string{"custom"},
		metricNames(resourceMetrics.ScopeMetrics[1]),
	)
}

func metricNames(scopeMetrics metricdata.ScopeMetrics) []string {
	names := make([]string, len(scopeMetrics.Metrics))
	for i, m := range scopeMetrics.Metrics {
		names[i] = m.Name
	}
	return names
}

// TestServiceTelemetryZPages verifies that the zpages extension works correctly with servce telemetry.
func TestServiceTelemetryZPages(t *testing.T) {
	t.Run("ipv4", func(t *testing.T) {
		testZPages(t, testutil.GetAvailableLocalAddress(t))
	})
	t.Run("ipv6", func(t *testing.T) {
		testZPages(t, testutil.GetAvailableLocalIPv6Address(t))
	})
}

func testZPages(t *testing.T, zpagesAddr string) {
	set := newNopSettings()
	set.BuildInfo = component.BuildInfo{Version: "test version", Command: otelCommand}
	set.ExtensionsConfigs = map[component.ID]component.Config{
		component.MustNewID("zpages"): &zpagesextension.Config{
			ServerConfig: confighttp.ServerConfig{Endpoint: zpagesAddr},
		},
	}
	set.ExtensionsFactories = map[component.Type]extension.Factory{
		component.MustNewType("zpages"): zpagesextension.NewFactory(),
	}

	cfg := newNopConfig()
	cfg.Extensions = []component.ID{component.MustNewID("zpages")}

	// The zpages extension will register/unregister a span processor with
	// the tracer provider, so long as it implements the right methods,
	// like the opentelemetry-go SDK implementation.
	registered := make(chan struct{}, 1)
	unregistered := make(chan struct{}, 1)
	set.TelemetryFactory = telemetry.NewFactory(
		func() component.Config { return nil },
		func(context.Context, telemetry.Settings, component.Config) (telemetry.Telemetry, error) {
			return telemetrytest.NewTelemetry(
				telemetrytest.WithTracerProvider(
					&registerableTracerProvider{
						TracerProvider: noop.NewTracerProvider(),
						registerSpanProcessor: func(sp sdktrace.SpanProcessor) {
							assert.IsType(t, sp, &zpages.SpanProcessor{})
							registered <- struct{}{}
						},
						unregisterSpanProcessor: func(sp sdktrace.SpanProcessor) {
							assert.IsType(t, sp, &zpages.SpanProcessor{})
							unregistered <- struct{}{}
						},
					},
				),
			), nil
		},
	)

	// Create a service, check for metrics, shutdown and repeat to ensure
	// that telemetry can be started/shutdown and started again.
	for i := 0; i < 2; i++ {
		srv, err := New(context.Background(), set, cfg)
		require.NoError(t, err)

		require.NoError(t, srv.Start(context.Background()))
		<-registered

		assert.Eventually(t, func() bool {
			return zpagesHealthy(zpagesAddr)
		}, 10*time.Second, 100*time.Millisecond, "zpages endpoint is not healthy")

		require.NoError(t, srv.Shutdown(context.Background()))
		<-unregistered
	}
}

type registerableTracerProvider struct {
	trace.TracerProvider
	registerSpanProcessor   func(sdktrace.SpanProcessor)
	unregisterSpanProcessor func(sdktrace.SpanProcessor)
}

func (p *registerableTracerProvider) RegisterSpanProcessor(sp sdktrace.SpanProcessor) {
	p.registerSpanProcessor(sp)
}

func (p *registerableTracerProvider) UnregisterSpanProcessor(sp sdktrace.SpanProcessor) {
	p.unregisterSpanProcessor(sp)
}

// TestServiceTelemetryRestart tests that the service starts and shuts down telemetry as expected.
func TestServiceTelemetryRestart(t *testing.T) {
	telemetryCreated := make(chan struct{}, 1)
	telemetryShutdown := make(chan struct{}, 1)

	set := newNopSettings()
	set.TelemetryFactory = telemetry.NewFactory(
		func() component.Config { return nil },
		func(context.Context, telemetry.Settings, component.Config) (telemetry.Telemetry, error) {
			telemetryCreated <- struct{}{}
			return telemetrytest.NewTelemetry(
				telemetrytest.WithShutdown(func(context.Context) error {
					telemetryShutdown <- struct{}{}
					return nil
				}),
			), nil
		},
	)

	for i := 0; i < 2; i++ {
		// Create and start a service, telemetry should be created.
		srv, err := New(context.Background(), set, newNopConfig())
		require.NoError(t, err)
		require.NoError(t, srv.Start(context.Background()))
		<-telemetryCreated

		// Shutdown the service, telemetry should be shutdown.
		require.NoError(t, srv.Shutdown(context.Background()))
		<-telemetryShutdown
	}
}

func TestExtensionNotificationFailure(t *testing.T) {
	set := newNopSettings()
	cfg := newNopConfig()

	extName := component.MustNewType("configWatcher")
	configWatcherExtensionFactory := newConfigWatcherExtensionFactory(extName)
	set.ExtensionsConfigs = map[component.ID]component.Config{component.NewID(extName): configWatcherExtensionFactory.CreateDefaultConfig()}
	set.ExtensionsFactories = map[component.Type]extension.Factory{extName: configWatcherExtensionFactory}
	cfg.Extensions = []component.ID{component.NewID(extName)}

	// Create a service
	srv, err := New(context.Background(), set, cfg)
	require.NoError(t, err)

	// Start the service
	require.Error(t, srv.Start(context.Background()))

	// Shut down the service
	require.NoError(t, srv.Shutdown(context.Background()))
}

func TestNilCollectorEffectiveConfig(t *testing.T) {
	set := newNopSettings()
	set.CollectorConf = nil
	cfg := newNopConfig()

	extName := component.MustNewType("configWatcher")
	configWatcherExtensionFactory := newConfigWatcherExtensionFactory(extName)
	set.ExtensionsConfigs = map[component.ID]component.Config{component.NewID(extName): configWatcherExtensionFactory.CreateDefaultConfig()}
	set.ExtensionsFactories = map[component.Type]extension.Factory{extName: configWatcherExtensionFactory}
	cfg.Extensions = []component.ID{component.NewID(extName)}

	// Create a service
	srv, err := New(context.Background(), set, cfg)
	require.NoError(t, err)

	// Start the service
	require.NoError(t, srv.Start(context.Background()))

	// Shut down the service
	require.NoError(t, srv.Shutdown(context.Background()))
}

func TestServiceTelemetryLogger(t *testing.T) {
	srv, err := New(context.Background(), newNopSettings(), newNopConfig())
	require.NoError(t, err)

	assert.NoError(t, srv.Start(context.Background()))
	t.Cleanup(func() {
		assert.NoError(t, srv.Shutdown(context.Background()))
	})
	assert.NotNil(t, srv.telemetrySettings.Logger)
}

func TestServiceFatalError(t *testing.T) {
	set := newNopSettings()
	set.AsyncErrorChannel = make(chan error)

	srv, err := New(context.Background(), set, newNopConfig())
	require.NoError(t, err)

	assert.NoError(t, srv.Start(context.Background()))
	t.Cleanup(func() {
		assert.NoError(t, srv.Shutdown(context.Background()))
	})

	go func() {
		ev := componentstatus.NewFatalErrorEvent(assert.AnError)
		srv.host.NotifyComponentStatusChange(&componentstatus.InstanceID{}, ev)
	}()

	err = <-srv.host.AsyncErrorChannel

	require.ErrorIs(t, err, assert.AnError)
}

func TestServiceTelemetryFactoryError(t *testing.T) {
	set := newNopSettings()
	set.TelemetryFactory = telemetry.NewFactory(
		func() component.Config { return nil },
		func(context.Context, telemetry.Settings, component.Config) (telemetry.Telemetry, error) {
			return nil, errors.New("something went wrong")
		},
	)

	cfg := newNopConfig()
	_, err := New(context.Background(), set, cfg)
	require.EqualError(t, err, "failed to create telemetry providers: something went wrong")
}

func zpagesHealthy(zpagesAddr string) bool {
	paths := []string{
		"/debug/tracez",
		"/debug/pipelinez",
		"/debug/servicez",
		"/debug/extensionz",
	}

	for _, path := range paths {
		resp, err := http.Get("http://" + zpagesAddr + path)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
	}
	return true
}

func newNopSettings() Settings {
	receiversConfigs, receiversFactories := builders.NewNopReceiverConfigsAndFactories()
	processorsConfigs, processorsFactories := builders.NewNopProcessorConfigsAndFactories()
	connectorsConfigs, connectorsFactories := builders.NewNopConnectorConfigsAndFactories()
	exportersConfigs, exportersFactories := builders.NewNopExporterConfigsAndFactories()
	extensionsConfigs, extensionsFactories := builders.NewNopExtensionConfigsAndFactories()
	telemetryFactory := telemetry.NewFactory(
		func() component.Config { return nil },
		func(context.Context, telemetry.Settings, component.Config) (telemetry.Telemetry, error) {
			return telemetrytest.NewTelemetry(), nil
		},
	)

	return Settings{
		BuildInfo:           component.NewDefaultBuildInfo(),
		CollectorConf:       confmap.New(),
		ReceiversConfigs:    receiversConfigs,
		ReceiversFactories:  receiversFactories,
		ProcessorsConfigs:   processorsConfigs,
		ProcessorsFactories: processorsFactories,
		ExportersConfigs:    exportersConfigs,
		ExportersFactories:  exportersFactories,
		ConnectorsConfigs:   connectorsConfigs,
		ConnectorsFactories: connectorsFactories,
		ExtensionsConfigs:   extensionsConfigs,
		ExtensionsFactories: extensionsFactories,
		TelemetryFactory:    telemetryFactory,
		AsyncErrorChannel:   make(chan error),
	}
}

func newNopConfig() Config {
	return newNopConfigPipelineConfigs(pipelines.Config{
		pipeline.NewID(pipeline.SignalTraces): {
			Receivers:  []component.ID{component.NewID(nopType)},
			Processors: []component.ID{component.NewID(nopType)},
			Exporters:  []component.ID{component.NewID(nopType)},
		},
		pipeline.NewID(pipeline.SignalMetrics): {
			Receivers:  []component.ID{component.NewID(nopType)},
			Processors: []component.ID{component.NewID(nopType)},
			Exporters:  []component.ID{component.NewID(nopType)},
		},
		pipeline.NewID(pipeline.SignalLogs): {
			Receivers:  []component.ID{component.NewID(nopType)},
			Processors: []component.ID{component.NewID(nopType)},
			Exporters:  []component.ID{component.NewID(nopType)},
		},
		pipeline.NewID(xpipeline.SignalProfiles): {
			Receivers:  []component.ID{component.NewID(nopType)},
			Processors: []component.ID{component.NewID(nopType)},
			Exporters:  []component.ID{component.NewID(nopType)},
		},
	})
}

func newNopConfigPipelineConfigs(pipelineCfgs pipelines.Config) Config {
	return Config{
		Extensions: extensions.Config{component.NewID(nopType)},
		Pipelines:  pipelineCfgs,
		Telemetry:  nil,
	}
}

type configWatcherExtension struct{}

func (comp *configWatcherExtension) Start(context.Context, component.Host) error {
	return nil
}

func (comp *configWatcherExtension) Shutdown(context.Context) error {
	return nil
}

func (comp *configWatcherExtension) NotifyConfig(context.Context, *confmap.Conf) error {
	return errors.New("Failed to resolve config")
}

func newConfigWatcherExtensionFactory(name component.Type) extension.Factory {
	return extension.NewFactory(
		name,
		func() component.Config {
			return &struct{}{}
		},
		func(context.Context, extension.Settings, component.Config) (extension.Extension, error) {
			return &configWatcherExtension{}, nil
		},
		component.StabilityLevelDevelopment,
	)
}

func newPtr[T int | string](str T) *T {
	return &str
}

func TestValidateGraph(t *testing.T) {
	testCases := map[string]struct {
		connectorCfg  map[component.ID]component.Config
		receiverCfg   map[component.ID]component.Config
		exporterCfg   map[component.ID]component.Config
		pipelinesCfg  pipelines.Config
		expectedError string
	}{
		"Valid connector usage": {
			connectorCfg: map[component.ID]component.Config{
				component.NewIDWithName(nopType, "connector1"): &struct{}{},
			},
			receiverCfg: map[component.ID]component.Config{
				component.NewID(nopType): &struct{}{},
			},
			exporterCfg: map[component.ID]component.Config{
				component.NewID(nopType): &struct{}{},
			},
			pipelinesCfg: pipelines.Config{
				pipeline.NewIDWithName(pipeline.SignalLogs, "in"): {
					Receivers:  []component.ID{component.NewID(nopType)},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewIDWithName(nopType, "connector1")},
				},
				pipeline.NewIDWithName(pipeline.SignalLogs, "out"): {
					Receivers:  []component.ID{component.NewIDWithName(nopType, "connector1")},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewID(nopType)},
				},
			},
			expectedError: "",
		},
		"Valid without Connector": {
			receiverCfg: map[component.ID]component.Config{
				component.NewID(nopType): &struct{}{},
			},
			exporterCfg: map[component.ID]component.Config{
				component.NewID(nopType): &struct{}{},
			},
			pipelinesCfg: pipelines.Config{
				pipeline.NewIDWithName(pipeline.SignalLogs, "in"): {
					Receivers:  []component.ID{component.NewID(nopType)},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewID(nopType)},
				},
				pipeline.NewIDWithName(pipeline.SignalLogs, "out"): {
					Receivers:  []component.ID{component.NewID(nopType)},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewID(nopType)},
				},
			},
			expectedError: "",
		},
		"Connector used as exporter but not as receiver": {
			connectorCfg: map[component.ID]component.Config{
				component.NewIDWithName(nopType, "connector1"): &struct{}{},
			},
			receiverCfg: map[component.ID]component.Config{
				component.NewID(nopType): &struct{}{},
			},
			exporterCfg: map[component.ID]component.Config{
				component.NewID(nopType): &struct{}{},
			},
			pipelinesCfg: pipelines.Config{
				pipeline.NewIDWithName(pipeline.SignalLogs, "in1"): {
					Receivers:  []component.ID{component.NewID(nopType)},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewID(nopType)},
				},
				pipeline.NewIDWithName(pipeline.SignalLogs, "in2"): {
					Receivers:  []component.ID{component.NewID(nopType)},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewIDWithName(nopType, "connector1")},
				},
				pipeline.NewIDWithName(pipeline.SignalLogs, "out"): {
					Receivers:  []component.ID{component.NewID(nopType)},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewID(nopType)},
				},
			},
			expectedError: `failed to build pipelines: connector "nop/connector1" used as exporter in [logs/in2] pipeline but not used in any supported receiver pipeline`,
		},
		"Connector used as receiver but not as exporter": {
			connectorCfg: map[component.ID]component.Config{
				component.NewIDWithName(nopType, "connector1"): &struct{}{},
			},
			receiverCfg: map[component.ID]component.Config{
				component.NewID(nopType): &struct{}{},
			},
			exporterCfg: map[component.ID]component.Config{
				component.NewID(nopType): &struct{}{},
			},
			pipelinesCfg: pipelines.Config{
				pipeline.NewIDWithName(pipeline.SignalLogs, "in1"): {
					Receivers:  []component.ID{component.NewID(nopType)},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewID(nopType)},
				},
				pipeline.NewIDWithName(pipeline.SignalLogs, "in2"): {
					Receivers:  []component.ID{component.NewIDWithName(nopType, "connector1")},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewID(nopType)},
				},
				pipeline.NewIDWithName(pipeline.SignalLogs, "out"): {
					Receivers:  []component.ID{component.NewID(nopType)},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewID(nopType)},
				},
			},
			expectedError: `failed to build pipelines: connector "nop/connector1" used as receiver in [logs/in2] pipeline but not used in any supported exporter pipeline`,
		},
		"Connector creates direct cycle between pipelines": {
			connectorCfg: map[component.ID]component.Config{
				component.NewIDWithName(nopType, "forward"): &struct{}{},
			},
			receiverCfg: map[component.ID]component.Config{
				component.NewID(nopType): &struct{}{},
			},
			exporterCfg: map[component.ID]component.Config{
				component.NewID(nopType): &struct{}{},
			},
			pipelinesCfg: pipelines.Config{
				pipeline.NewIDWithName(pipeline.SignalTraces, "in"): {
					Receivers:  []component.ID{component.NewIDWithName(nopType, "forward")},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewIDWithName(nopType, "forward")},
				},
				pipeline.NewIDWithName(pipeline.SignalTraces, "out"): {
					Receivers:  []component.ID{component.NewIDWithName(nopType, "forward")},
					Processors: []component.ID{},
					Exporters:  []component.ID{component.NewIDWithName(nopType, "forward")},
				},
			},
			expectedError: `failed to build pipelines: cycle detected: connector "nop/forward" (traces to traces) -> connector "nop/forward" (traces to traces)`,
		},
	}

	_, connectorsFactories := builders.NewNopConnectorConfigsAndFactories()
	_, receiversFactories := builders.NewNopReceiverConfigsAndFactories()
	_, exportersFactories := builders.NewNopExporterConfigsAndFactories()

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			settings := Settings{
				ConnectorsConfigs:   tc.connectorCfg,
				ConnectorsFactories: connectorsFactories,
				ReceiversConfigs:    tc.receiverCfg,
				ReceiversFactories:  receiversFactories,
				ExportersConfigs:    tc.exporterCfg,
				ExportersFactories:  exportersFactories,
			}
			cfg := Config{
				Pipelines: tc.pipelinesCfg,
			}

			err := Validate(context.Background(), settings, cfg)
			if tc.expectedError == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, tc.expectedError, err.Error())
			}
		})
	}
}

type inmemLogsExporter struct {
	mu      sync.RWMutex
	records []sdklog.Record
}

func (e *inmemLogsExporter) GetRecords() []sdklog.Record {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return slices.Clone(e.records)
}

func (e *inmemLogsExporter) Export(ctx context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.records = append(e.records, records...)
	return nil
}

func (*inmemLogsExporter) Shutdown(context.Context) error {
	return nil
}

func (*inmemLogsExporter) ForceFlush(context.Context) error {
	return nil
}
