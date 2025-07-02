// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelconftelemetry

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/featuregate"
)

func TestFactory_CreateDefaultConfig(t *testing.T) {
	tests := []struct {
		expected string
		gate     bool
	}{
		{
			expected: "localhost",
			gate:     true,
		},
		{
			expected: "",
			gate:     false,
		},
	}

	for _, tt := range tests {
		t.Run("UseLocalHostAsDefaultMetricsAddress/"+strconv.FormatBool(tt.gate), func(t *testing.T) {
			setFeatureGateEnabled(t, useLocalHostAsDefaultMetricsAddressFeatureGate, tt.gate)

			cfg := NewFactory().CreateDefaultConfig()
			componenttest.CheckConfigStruct(cfg)

			require.Len(t, cfg.(*Config).Metrics.Readers, 1)
			assert.Equal(t, tt.expected, *cfg.(*Config).Metrics.Readers[0].Pull.Exporter.Prometheus.Host)
			assert.Equal(t, 8888, *cfg.(*Config).Metrics.Readers[0].Pull.Exporter.Prometheus.Port)
		})
	}
}

func setFeatureGateEnabled(t *testing.T, gate *featuregate.Gate, enabled bool) {
	t.Helper()
	prev := gate.IsEnabled()
	require.NoError(t, featuregate.GlobalRegistry().Set(gate.ID(), enabled))
	t.Cleanup(func() {
		// Restore previous value.
		require.NoError(t, featuregate.GlobalRegistry().Set(gate.ID(), prev))
	})
}
