// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package debugscraper // import "go.opentelemetry.io/collector/receiver/debugscraper"

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/scraper"
	"go.opentelemetry.io/collector/scraper/scraperhelper"

	"go.opentelemetry.io/collector/receiver/debugscraper/internal/metadata"
)

// NewFactory returns a receiver.Factory that constructs the debug scraper receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		metadata.Type,
		func() component.Config { return createDefaultConfig() },
		receiver.WithMetrics(createMetrics, component.StabilityLevelDevelopment),
	)
}

func createMetrics(_ context.Context, set receiver.Settings, cfg component.Config, next consumer.Metrics) (receiver.Metrics, error) {
	rcfg := cfg.(*Config)

	metricsScraper, err := scraper.NewMetrics(func(context.Context) (pmetric.Metrics, error) {
		md := pmetric.NewMetrics()
		rm := md.ResourceMetrics().AppendEmpty()
		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName(metadata.ScopeName)
		m := sm.Metrics().AppendEmpty()
		m.SetName("debug_metric")
		gauge := m.SetEmptyGauge()
		dp := gauge.DataPoints().AppendEmpty()
		dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
		dp.SetIntValue(1)
		return md, nil
	})
	if err != nil {
		return nil, err
	}

	return scraperhelper.NewMetricsController(
		&rcfg.Controller,
		set,
		next,
		scraperhelper.AddScraper(metadata.Type, metricsScraper),
	)
}
