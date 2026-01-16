// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package scrapercontroller // import "go.opentelemetry.io/collector/extension/xextension/scrapercontroller"

import (
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/scraper"
)

type MetricsScraperRegistry interface {
	// RegisterMetrics registers a metrics scraper and consumer.
	RegisterMetrics(scraper.Metrics, consumer.Metrics) (component.ShutdownFunc, error)
}

type LogsScraperRegistry interface {
	// RegisterLogs registers a logs scraper and consumer.
	RegisterLogs(scraper.Logs, consumer.Logs) (component.ShutdownFunc, error)
}
