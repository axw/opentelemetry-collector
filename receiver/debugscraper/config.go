// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package debugscraper // import "go.opentelemetry.io/collector/receiver/debugscraper"

import (
	"go.opentelemetry.io/collector/scraper/scraperhelper"
)

// Config configures the debug scraper receiver.
type Config struct {
	// Controller configures how scrapers are triggered.
	Controller scraperhelper.ControllerConfig `mapstructure:",squash"`

	// prevent unkeyed literal initialization
	_ struct{}
}

func createDefaultConfig() *Config {
	return &Config{
		Controller: scraperhelper.NewDefaultControllerConfig(),
	}
}
