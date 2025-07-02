// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelconftelemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	config "go.opentelemetry.io/contrib/otelconf/v0.3.0"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"go.opentelemetry.io/collector/service/telemetry"
)

func TestTelemetry_Resource(t *testing.T) {
	type testcase struct {
		resourceConfig        map[string]*string
		expectedResourceAttrs map[string]any
	}

	tests := map[string]testcase{
		"default": {
			expectedResourceAttrs: map[string]any{
				"service.name":        "otelcol",
				"service.version":     "latest",
				"service.instance.id": "<generated>",
			},
		},
		"configured": {
			resourceConfig: map[string]*string{
				"service.name":        ptr("custom-service"),
				"service.version":     nil, // removes the field
				"service.instance.id": ptr("custom-instance-id"),
				"custom.field":        ptr("custom-value"),
			},
			expectedResourceAttrs: map[string]any{
				"service.name":        "custom-service",
				"service.instance.id": "custom-instance-id",
				"custom.field":        "custom-value",
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.Resource = test.resourceConfig
			tel, _ := newTestTelemetry(t, cfg)
			res := tel.Resource()

			attrs := res.Attributes().AsRaw()
			if _, ok := test.resourceConfig["service.instance.id"]; !ok {
				// If the service.instance.id is not configured, it should be auto-generated.
				assert.Contains(t, attrs, "service.instance.id")
				attrs["service.instance.id"] = "<generated>"
			}
			assert.Equal(t, test.expectedResourceAttrs, attrs)
		})
	}
}

func TestTelemetry_Logger(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Resource = map[string]*string{
		"service.instance.id": ptr("instance-id"),
	}
	tel, observedLogs := newTestTelemetry(t, cfg)
	tel.Logger().Info("message", zap.String("key", "value"))

	entries := observedLogs.All()
	require.Len(t, entries, 1)
	assert.Equal(t, "message", entries[0].Message)
	assert.Equal(t, map[string]any{
		"resource": map[string]any{
			"service.name":        "otelcol",
			"service.version":     "latest",
			"service.instance.id": "instance-id",
		},
		"key": "value",
	}, entries[0].ContextMap())
}

func TestTelemetry_LoggerProvider(t *testing.T) {
	var received []plog.Logs
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/logs", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		assert.NoError(t, err)

		exportRequest := plogotlp.NewExportRequest()
		assert.NoError(t, exportRequest.UnmarshalProto(body))
		received = append(received, exportRequest.Logs())
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.Resource = map[string]*string{
		"service.instance.id": ptr("instance-id"),
		"extra":               ptr("value"),
	}
	cfg.Logs.Level = zapcore.InfoLevel // configures filtering on the LoggerProvider
	cfg.Logs.Processors = []config.LogRecordProcessor{
		newOTLPSimpleLogRecordProcessor(t, srv),
	}
	tel, _ := newTestTelemetry(t, cfg)

	// Logs produced with the zap.Logger are not sent to the LoggerProvider.
	// This is done at the service level, so it is independent of the
	// telemetry implementation.
	tel.Logger().Info("message1", zap.String("key", "value"))

	logger := tel.LoggerProvider().Logger("test_logger")
	record := log.Record{}
	record.SetBody(log.StringValue("message2"))
	record.AddAttributes(log.String("key", "value"))
	logger.Emit(context.Background(), record)

	// `level: info` was configured, so debug logs should be filtered out.
	assert.False(t, logger.Enabled(context.Background(), log.EnabledParameters{
		Severity: log.SeverityDebug,
	}))

	require.Len(t, received, 1)
	logs := received[0]
	require.Equal(t, 1, logs.LogRecordCount())
	assert.Equal(t, map[string]any{
		"service.name":        "otelcol",
		"service.version":     "latest",
		"service.instance.id": "instance-id",
		"extra":               "value",
	}, logs.ResourceLogs().At(0).Resource().Attributes().AsRaw())

	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	assert.Equal(t, "message2", logRecord.Body().Str())
}

func TestTelemetry_MeterProvider(t *testing.T) {
	var received []pmetric.Metrics
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/metrics", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		assert.NoError(t, err)

		exportRequest := pmetricotlp.NewExportRequest()
		assert.NoError(t, exportRequest.UnmarshalProto(body))
		received = append(received, exportRequest.Metrics())
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.Resource = map[string]*string{
		"service.instance.id": ptr("instance-id"),
		"extra":               ptr("value"),
	}
	cfg.Metrics.Readers = []config.MetricReader{{
		Periodic: &config.PeriodicMetricReader{
			Exporter: config.PushMetricExporter{
				OTLP: &config.OTLPMetric{
					Endpoint: ptr(srv.URL),
					Protocol: ptr("http/protobuf"),
					Insecure: ptr(true),
				},
			},
		},
	}}

	tel, _ := newTestTelemetry(t, cfg)
	meter := tel.MeterProvider().Meter("test_meter")
	counter, _ := meter.Int64Counter("counter")
	counter.Add(context.Background(), 1)
	assert.NoError(t, tel.Shutdown(context.Background())) // flush metrics

	require.Len(t, received, 1)
	metrics := received[0]
	require.Equal(t, 1, metrics.DataPointCount())
	assert.Equal(t, map[string]any{
		"service.name":        "otelcol",
		"service.version":     "latest",
		"service.instance.id": "instance-id",
		"extra":               "value",
	}, metrics.ResourceMetrics().At(0).Resource().Attributes().AsRaw())
}

func TestTelemetry_TracerProvider(t *testing.T) {
	var received []ptrace.Traces
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		assert.NoError(t, err)

		exportRequest := ptraceotlp.NewExportRequest()
		assert.NoError(t, exportRequest.UnmarshalProto(body))
		received = append(received, exportRequest.Traces())
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.Resource = map[string]*string{
		"service.instance.id": ptr("instance-id"),
		"extra":               ptr("value"),
	}
	cfg.Traces.Processors = []config.SpanProcessor{
		newOTLPSimpleSpanProcessor(t, srv),
	}

	tel, _ := newTestTelemetry(t, cfg)
	tracer := tel.TracerProvider().Tracer("test_tracer")
	_, span := tracer.Start(context.Background(), "test_span")
	span.End()

	require.Len(t, received, 1)
	traces := received[0]
	require.Equal(t, 1, traces.SpanCount())
	assert.Equal(t, map[string]any{
		"service.name":        "otelcol",
		"service.version":     "latest",
		"service.instance.id": "instance-id",
		"extra":               "value",
	}, traces.ResourceSpans().At(0).Resource().Attributes().AsRaw())
}

func TestTelemetry_TracerProvider_Propagators(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, req *http.Request) {})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.Traces.Propagators = []string{"b3", "tracecontext"}
	cfg.Traces.Processors = []config.SpanProcessor{
		newOTLPSimpleSpanProcessor(t, srv),
	}

	tel, _ := newTestTelemetry(t, cfg)
	propagator := otel.GetTextMapPropagator()
	require.NotNil(t, propagator)

	tracer := tel.TracerProvider().Tracer("test_tracer")
	ctx, span := tracer.Start(context.Background(), "test_span")
	mapCarrier := make(propagation.MapCarrier)
	propagator.Inject(ctx, mapCarrier)
	span.End()

	assert.Contains(t, mapCarrier, "b3")
	assert.Contains(t, mapCarrier, "traceparent")
}

func TestTelemetry_TracerProviderDisabled(t *testing.T) {
	test := func(t *testing.T, cfg *Config) {
		t.Helper()

		var received int
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, req *http.Request) {
			received++
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		cfg.Traces.Processors = []config.SpanProcessor{
			newOTLPSimpleSpanProcessor(t, srv),
		}

		tel, _ := newTestTelemetry(t, cfg)
		tracer := tel.TracerProvider().Tracer("test_tracer")
		_, span := tracer.Start(context.Background(), "test_span")
		span.End()
		assert.NoError(t, tel.Shutdown(context.Background()))
		assert.Equal(t, 0, received)
	}

	t.Run("level_none", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.Traces.Level = configtelemetry.LevelNone
		test(t, cfg)
	})
	t.Run("noop_tracer_gate", func(t *testing.T) {
		setFeatureGateEnabled(t, noopTracerProvider, true)
		cfg := createDefaultConfig().(*Config)
		cfg.Traces.Level = configtelemetry.LevelBasic
		test(t, cfg)
	})
}

func newOTLPSimpleLogRecordProcessor(t *testing.T, srv *httptest.Server) config.LogRecordProcessor {
	return config.LogRecordProcessor{
		Simple: &config.SimpleLogRecordProcessor{
			Exporter: config.LogRecordExporter{
				OTLP: &config.OTLP{
					Endpoint: ptr(srv.URL),
					Protocol: ptr("http/protobuf"),
					Insecure: ptr(true),
				},
			},
		},
	}
}

func newOTLPSimpleSpanProcessor(t *testing.T, srv *httptest.Server) config.SpanProcessor {
	return config.SpanProcessor{
		Simple: &config.SimpleSpanProcessor{
			Exporter: config.SpanExporter{
				OTLP: &config.OTLP{
					Endpoint: ptr(srv.URL),
					Protocol: ptr("http/protobuf"),
					Insecure: ptr(true),
				},
			},
		},
	}
}

func newTestTelemetry(t *testing.T, cfg *Config) (telemetry.Telemetry, *observer.ObservedLogs) {
	t.Helper()

	core, observedLogs := observer.New(zapcore.DebugLevel)
	set := telemetry.Settings{
		BuildInfo: component.NewDefaultBuildInfo(),
		ZapOptions: []zap.Option{
			zap.WrapCore(func(zapcore.Core) zapcore.Core { return core }),
		},
	}

	if len(cfg.Metrics.Readers) == 1 && cfg.Metrics.Readers[0].Pull != nil &&
		cfg.Metrics.Readers[0].Pull.Exporter.Prometheus != nil &&
		cfg.Metrics.Readers[0].Pull.Exporter.Prometheus.Port != nil &&
		*cfg.Metrics.Readers[0].Pull.Exporter.Prometheus.Port == 8888 {
		// Replace the default port with 0 to bind to an ephemeral port,
		// avoiding flaky tests.
		*cfg.Metrics.Readers[0].Pull.Exporter.Prometheus.Port = 0
	}

	factory := NewFactory()
	telemetry, err := factory.CreateTelemetry(context.Background(), set, cfg)
	require.NoError(t, err)
	require.NotNil(t, telemetry)
	t.Cleanup(func() {
		assert.NoError(t, telemetry.Shutdown(context.Background()))
	})
	return telemetry, observedLogs
}
