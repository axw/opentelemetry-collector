// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package timescrapercontroller // import "go.opentelemetry.io/collector/extension/xextension/scrapercontroller/timescrapercontroller"

import (
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/confmap/xconfmap"
)

var errNonPositive = errors.New("requires positive value")

// Config configures the time-based scraper controller extension.
type Config struct {
	// CollectionInterval sets how frequently registered scrapers should be triggered.
	CollectionInterval time.Duration `mapstructure:"collection_interval"`

	// InitialDelay sets the initial start delay. Any non-positive value is treated as immediate.
	InitialDelay time.Duration `mapstructure:"initial_delay"`

	// Timeout is an optional value used as the context deadline when calling Scrape*.
	Timeout time.Duration `mapstructure:"timeout"`

	// prevent unkeyed literal initialization
	_ struct{}
}

var _ xconfmap.Validator = (*Config)(nil)

func (c *Config) Validate() error {
	if c.CollectionInterval <= 0 {
		return fmt.Errorf(`"collection_interval": %w`, errNonPositive)
	}
	if c.Timeout < 0 {
		return fmt.Errorf(`"timeout": %w`, errNonPositive)
	}
	return nil
}

func createDefaultConfig() *Config {
	return &Config{
		CollectionInterval: time.Minute,
		InitialDelay:       time.Second,
		Timeout:            0,
	}
}

