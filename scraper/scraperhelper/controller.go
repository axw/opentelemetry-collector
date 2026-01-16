// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package scraperhelper // import "go.opentelemetry.io/collector/scraper/scraperhelper"

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pipeline"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/receiverhelper"
	"go.opentelemetry.io/collector/scraper"
	"go.opentelemetry.io/collector/scraper/scrapererror"
	"go.opentelemetry.io/collector/scraper/scraperhelper/internal/controller"
)

type ControllerConfig = controller.ControllerConfig

type metricsScraperRegistry interface {
	RegisterMetrics(scraper.Metrics, consumer.Metrics) (component.ShutdownFunc, error)
}

type logsScraperRegistry interface {
	RegisterLogs(scraper.Logs, consumer.Logs) (component.ShutdownFunc, error)
}

type obsMetricsConsumer struct {
	next consumer.Metrics
	obs  *receiverhelper.ObsReport
}

func (o obsMetricsConsumer) Capabilities() consumer.Capabilities {
	return o.next.Capabilities()
}

func (o obsMetricsConsumer) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	dataPointCount := md.DataPointCount()
	ctx = o.obs.StartMetricsOp(ctx)
	err := o.next.ConsumeMetrics(ctx, md)
	o.obs.EndMetricsOp(ctx, "", dataPointCount, err)
	return err
}

// NewDefaultControllerConfig returns default scraper controller
// settings with a collection interval of one minute.
func NewDefaultControllerConfig() ControllerConfig {
	return controller.NewDefaultControllerConfig()
}

// ControllerOption apply changes to internal options.
type ControllerOption interface {
	apply(*controllerOptions)
}

type optionFunc func(*controllerOptions)

func (of optionFunc) apply(e *controllerOptions) {
	of(e)
}

// AddScraper configures the scraper.Metrics to be called with the specified options,
// and at the specified collection interval.
//
// Observability information will be reported, and the scraped metrics
// will be passed to the next consumer.
func AddScraper(t component.Type, sc scraper.Metrics) ControllerOption {
	f := scraper.NewFactory(t, nil,
		scraper.WithMetrics(func(context.Context, scraper.Settings, component.Config) (scraper.Metrics, error) {
			return sc, nil
		}, component.StabilityLevelAlpha))
	return AddFactoryWithConfig(f, nil)
}

// AddFactoryWithConfig configures the scraper.Factory and associated config that
// will be used to create a new scraper. The created scraper will be called with
// the specified options, and at the specified collection interval.
//
// Observability information will be reported, and the scraped metrics
// will be passed to the next consumer.
func AddFactoryWithConfig(f scraper.Factory, cfg component.Config) ControllerOption {
	return optionFunc(func(o *controllerOptions) {
		o.factoriesWithConfig = append(o.factoriesWithConfig, factoryWithConfig{f: f, cfg: cfg})
	})
}

// WithTickerChannel allows you to override the scraper controller's ticker
// channel to specify when scrape is called. This is only expected to be
// used by tests.
func WithTickerChannel(tickerCh <-chan time.Time) ControllerOption {
	return optionFunc(func(o *controllerOptions) {
		o.tickerCh = tickerCh
	})
}

type factoryWithConfig struct {
	f   scraper.Factory
	cfg component.Config
}

type controllerOptions struct {
	tickerCh            <-chan time.Time
	factoriesWithConfig []factoryWithConfig
}

// NewLogsController creates a receiver.Logs with the configured options, that can control multiple scraper.Logs.
func NewLogsController(cfg *ControllerConfig,
	rSet receiver.Settings,
	nextConsumer consumer.Logs,
	options ...ControllerOption,
) (receiver.Logs, error) {
	co := getOptions(options)
	scrapers := make([]scraper.Logs, 0, len(co.factoriesWithConfig))
	for _, fwc := range co.factoriesWithConfig {
		set := controller.GetSettings(fwc.f.Type(), rSet)
		s, err := fwc.f.CreateLogs(context.Background(), set, fwc.cfg)
		if err != nil {
			return nil, err
		}
		s, err = wrapObsLogs(s, rSet.ID, set.ID, set.TelemetrySettings)
		if err != nil {
			return nil, err
		}
		scrapers = append(scrapers, s)
	}
	scrapeFunc := func(c *controller.Controller[scraper.Logs]) {
		scrapeLogs(c, nextConsumer)
	}
	startExternalController := createStartExternalController(
		scrapers, nextConsumer, logsScraperRegistry.RegisterLogs, pipeline.SignalLogs,
	)
	c, err := controller.NewController[scraper.Logs](
		cfg, rSet, scrapers, scrapeFunc, startExternalController, co.tickerCh,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// NewMetricsController creates a receiver.Metrics with the configured options, that can control multiple scraper.Metrics.
func NewMetricsController(cfg *ControllerConfig,
	rSet receiver.Settings,
	nextConsumer consumer.Metrics,
	options ...ControllerOption,
) (receiver.Metrics, error) {
	co := getOptions(options)
	scrapers := make([]scraper.Metrics, 0, len(co.factoriesWithConfig))
	for _, fwc := range co.factoriesWithConfig {
		set := controller.GetSettings(fwc.f.Type(), rSet)
		s, err := fwc.f.CreateMetrics(context.Background(), set, fwc.cfg)
		if err != nil {
			return nil, err
		}
		s, err = wrapObsMetrics(s, rSet.ID, set.ID, set.TelemetrySettings)
		if err != nil {
			return nil, err
		}
		scrapers = append(scrapers, s)
	}
	scrapeFunc := func(c *controller.Controller[scraper.Metrics]) {
		scrapeMetrics(c, nextConsumer)
	}
	startExternalController := createStartExternalController(
		scrapers, nextConsumer, metricsScraperRegistry.RegisterMetrics, pipeline.SignalMetrics,
	)
	c, err := controller.NewController[scraper.Metrics](
		cfg, rSet, scrapers, scrapeFunc, startExternalController, co.tickerCh,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func createStartExternalController[ScraperT, RegistryT, ConsumerT any](
	scrapers []ScraperT, nextConsumer ConsumerT,
	registerScraper func(registry RegistryT, scraper ScraperT, consumer ConsumerT) (component.ShutdownFunc, error),
	signal pipeline.Signal,
) func(ctx context.Context, host component.Host, id component.ID) (component.ShutdownFunc, error) {
	return func(ctx context.Context, host component.Host, id component.ID) (component.ShutdownFunc, error) {
		exts := host.GetExtensions()
		if exts == nil {
			return nil, errors.New("host does not support extensions")
		}
		ext, ok := exts[id]
		if !ok {
			return nil, fmt.Errorf("controller extension %q not found", id)
		}
		registry, ok := ext.(RegistryT)
		if !ok {
			return nil, fmt.Errorf("extension %q does not support controlling %s scrapers", id, signal)
		}
		shutdownFuncs := make([]component.ShutdownFunc, len(scrapers))
		for i, scraper := range scrapers {
			shutdown, err := registerScraper(registry, scraper, nextConsumer)
			if err != nil {
				// TODO unregister previously registered scrapers.
				return nil, err
			}
			shutdownFuncs[i] = shutdown
		}
		return func(ctx context.Context) error {
			var err error
			for _, shutdown := range shutdownFuncs {
				if shutdownErr := shutdown(ctx); shutdownErr != nil {
					err = errors.Join(err, shutdownErr)
				}
			}
			return err
		}, nil
	}
}

func scrapeLogs(c *controller.Controller[scraper.Logs], nextConsumer consumer.Logs) {
	ctx, done := controller.WithScrapeContext(c.Timeout)
	defer done()

	logs := plog.NewLogs()
	for i := range c.Scrapers {
		md, err := c.Scrapers[i].ScrapeLogs(ctx)
		if err != nil && !scrapererror.IsPartialScrapeError(err) {
			continue
		}
		md.ResourceLogs().MoveAndAppendTo(logs.ResourceLogs())
	}

	logRecordCount := logs.LogRecordCount()
	ctx = c.Obsrecv.StartMetricsOp(ctx)
	err := nextConsumer.ConsumeLogs(ctx, logs)
	c.Obsrecv.EndMetricsOp(ctx, "", logRecordCount, err)
}

func scrapeMetrics(c *controller.Controller[scraper.Metrics], nextConsumer consumer.Metrics) {
	ctx, done := controller.WithScrapeContext(c.Timeout)
	defer done()

	metrics := pmetric.NewMetrics()
	for i := range c.Scrapers {
		md, err := c.Scrapers[i].ScrapeMetrics(ctx)
		if err != nil && !scrapererror.IsPartialScrapeError(err) {
			continue
		}
		md.ResourceMetrics().MoveAndAppendTo(metrics.ResourceMetrics())
	}

	dataPointCount := metrics.DataPointCount()
	ctx = c.Obsrecv.StartMetricsOp(ctx)
	err := nextConsumer.ConsumeMetrics(ctx, metrics)
	c.Obsrecv.EndMetricsOp(ctx, "", dataPointCount, err)
}

func getOptions(options []ControllerOption) controllerOptions {
	co := controllerOptions{}
	for _, op := range options {
		op.apply(&co)
	}
	return co
}
