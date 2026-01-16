// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package httpscrapercontroller // import "go.opentelemetry.io/collector/extension/xextension/scrapercontroller/httpscrapercontroller"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"

	"go.opentelemetry.io/collector/extension/xextension/scrapercontroller/httpscrapercontroller/internal/metadata"
)

// NewFactory creates a factory for the HTTP scraper controller extension.
func NewFactory() extension.Factory {
	return extension.NewFactory(
		metadata.Type,
		func() component.Config { return createDefaultConfig() },
		create,
		component.StabilityLevelAlpha,
	)
}

func create(_ context.Context, set extension.Settings, cfg component.Config) (extension.Extension, error) {
	return newHTTPScraperController(cfg.(*Config), set.TelemetrySettings, set.Logger), nil
}
