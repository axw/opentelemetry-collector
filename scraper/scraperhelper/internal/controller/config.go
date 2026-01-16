// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package controller // import "go.opentelemetry.io/collector/scraper/scraperhelper/internal/controller"

import (
	"errors"
	"fmt"
	"time"

	"go.uber.org/multierr"

	"go.opentelemetry.io/collector/component"
)

var errNonPositiveInterval = errors.New("requires positive value")
var errMissingControllers = errors.New(`must be set when "collection_interval" is zero`)

// ControllerConfig defines common settings for a scraper controller
// configuration. Scraper controller receivers can embed this struct, instead
// of receiver.Settings, and extend it with more fields if needed.
type ControllerConfig struct {
	// CollectionInterval sets how frequently the scraper
	// should be called and used as the context timeout
	// to ensure that scrapers don't exceed the interval.
	//
	// If CollectionInterval is explicitly set to zero, the
	// built-in timer controller is disabled and scraping is
	// expected to be driven by external controller extensions.
	//
	//
	// Defaults to 1 minute.
	CollectionInterval time.Duration `mapstructure:"collection_interval"`

	// InitialDelay sets the initial start delay for the scraper,
	// any non positive value is assumed to be immediately.
	//
	// Defaults to 1 second.
	InitialDelay time.Duration `mapstructure:"initial_delay"`

	// Timeout is an optional value used to set scraper's context deadline.
	//
	// This is only used by the built-in time-based controller,
	// and not by external controller extensions.
	Timeout time.Duration `mapstructure:"timeout"`

	// Controllers is a list of extension IDs that control scraper execution when the built-in
	// controller is disabled.
	Controllers []component.ID `mapstructure:"controllers"`

	// prevent unkeyed literal initialization
	_ struct{}
}

// NewDefaultControllerConfig returns default scraper controller
// settings with a collection interval of one minute.
func NewDefaultControllerConfig() ControllerConfig {
	return ControllerConfig{
		CollectionInterval: time.Minute,
		InitialDelay:       time.Second,
		Timeout:            0,
	}
}

func (set *ControllerConfig) Validate() (errs error) {
	if set.CollectionInterval < 0 {
		errs = multierr.Append(errs, fmt.Errorf(`"collection_interval": %w`, errNonPositiveInterval))
	} else if set.CollectionInterval == 0 && len(set.Controllers) == 0 {
		errs = multierr.Append(errs, fmt.Errorf(`"controllers": %w`, errMissingControllers))
	}
	if set.Timeout < 0 {
		errs = multierr.Append(errs, fmt.Errorf(`"timeout": %w`, errNonPositiveInterval))
	}
	return errs
}
