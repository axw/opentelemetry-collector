// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelconftelemetry

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	config "go.opentelemetry.io/contrib/otelconf/v0.3.0"

	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/confmap/confmaptest"
	"go.opentelemetry.io/collector/confmap/xconfmap"
)

func TestConfig(t *testing.T) {
	t.Parallel()

	type testcase struct {
		setup        func(*testing.T) // optional testcase setup
		config       *Config
		unmarshalErr string
		validateErr  string
	}

	tests := map[string]testcase{
		"config_empty.yaml": {
			config: createDefaultConfig().(*Config),
		},
		"config_logs.yaml": {
			config: func() *Config {
				cfg := createDefaultConfig().(*Config)
				cfg.Logs.Development = true
				cfg.Logs.DisableCaller = true
				cfg.Logs.DisableStacktrace = true
				cfg.Logs.InitialFields = map[string]any{"fieldKey": "fieldValue"}
				cfg.Logs.Level = zap.InfoLevel
				cfg.Logs.Sampling = &LogsSamplingConfig{
					Enabled:    false,
					Tick:       1 * time.Second,
					Initial:    234,
					Thereafter: 567,
				}
				cfg.Logs.Processors = []config.LogRecordProcessor{{
					Batch: &config.BatchLogRecordProcessor{
						Exporter: config.LogRecordExporter{
							Console: config.Console{},
						},
					},
				}}
				return cfg
			}(),
		},
		"config_metrics_empty_readers.yaml": {
			config: func() *Config {
				cfg := createDefaultConfig().(*Config)
				cfg.Metrics.Level = configtelemetry.LevelNone
				cfg.Metrics.Readers = []config.MetricReader{}
				return cfg
			}(),
		},
		"config_invalid_unknown_field.yaml": {
			unmarshalErr: `invalid keys: unknown`,
		},
		"config_invalid_metrics_empty_readers.yaml": {
			validateErr: `collector telemetry metrics reader should exist when metric level is not none`,
		},
		"config_invalid_metrics_views_level.yaml": {
			validateErr: `service::telemetry::metrics::views can only be set when service::telemetry::metrics::level is detailed`,
		},
		"config_invalid_metrics_views_feature_gate.yaml": {
			setup: func(t *testing.T) {
				setFeatureGateEnabled(t, disableHighCardinalityMetricsFeatureGate, true)
			},
			validateErr: `telemetry.disableHighCardinalityMetrics gate is incompatible with service::telemetry::metrics::views`,
		},
	}

	for filename, test := range tests {
		t.Run(filename, func(t *testing.T) {
			if test.setup != nil {
				test.setup(t)
			}

			cm, err := confmaptest.LoadConf(filepath.Join("testdata", filename))
			require.NoError(t, err)

			cfg := createDefaultConfig().(*Config)
			err = cm.Unmarshal(cfg)
			if test.unmarshalErr != "" {
				assert.ErrorContains(t, err, test.unmarshalErr)
				return
			}
			require.NoError(t, err)

			err = xconfmap.Validate(cfg)
			if test.validateErr != "" {
				assert.ErrorContains(t, err, test.validateErr)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, test.config, cfg)
		})
	}
}
