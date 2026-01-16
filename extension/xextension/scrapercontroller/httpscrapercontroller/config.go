// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package httpscrapercontroller // import "go.opentelemetry.io/collector/extension/xextension/scrapercontroller/httpscrapercontroller"

import (
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/confmap/xconfmap"
)

type Config struct {
	// ServerConfig configures the HTTP server.
	confighttp.ServerConfig `mapstructure:",squash"`

	// Timeout is an optional context deadline used for both scrape and consume.
	Timeout time.Duration `mapstructure:"timeout"`

	// prevent unkeyed literal initialization
	_ struct{}
}

var _ xconfmap.Validator = (*Config)(nil)

func createDefaultConfig() *Config {
	sc := confighttp.NewDefaultServerConfig()
	sc.NetAddr.Endpoint = "localhost:0"
	return &Config{
		ServerConfig: sc,
		Timeout:      0,
	}
}

func (c *Config) Validate() error {
	if c.Timeout < 0 {
		return fmt.Errorf(`"timeout": %w`, errors.New("requires non-negative value"))
	}
	return nil
}
