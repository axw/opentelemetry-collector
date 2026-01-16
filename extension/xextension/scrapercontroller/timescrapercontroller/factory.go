// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package timescrapercontroller // import "go.opentelemetry.io/collector/extension/xextension/scrapercontroller/timescrapercontroller"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"

	"go.opentelemetry.io/collector/extension/xextension/scrapercontroller/timescrapercontroller/internal/metadata"
)

// NewFactory creates a factory for the time-based scraper controller extension.
func NewFactory() extension.Factory {
	return extension.NewFactory(
		metadata.Type,
		func() component.Config { return createDefaultConfig() },
		create,
		component.StabilityLevelAlpha,
	)
}

func create(_ context.Context, set extension.Settings, cfg component.Config) (extension.Extension, error) {
	return newTimeScraperController(cfg.(*Config), set.Logger), nil
}
