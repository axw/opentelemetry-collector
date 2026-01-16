// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package timescrapercontroller // import "go.opentelemetry.io/collector/extension/xextension/scrapercontroller/timescrapercontroller"

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/extension/xextension/scrapercontroller"
	"go.opentelemetry.io/collector/scraper"
)

type metricsRegistration struct {
	scraper  scraper.Metrics
	consumer consumer.Metrics
}

type logsRegistration struct {
	scraper  scraper.Logs
	consumer consumer.Logs
}

type timeScraperController struct {
	component.StartFunc
	component.ShutdownFunc

	cfg    *Config
	logger *zap.Logger

	idGen atomic.Uint64

	mu sync.RWMutex
	// Registered scraper+consumer pairs, keyed by registration id.
	metrics map[uint64]metricsRegistration
	logs    map[uint64]logsRegistration

	done chan struct{}
	wg   sync.WaitGroup
}

var (
	_ scrapercontroller.MetricsScraperRegistry = (*timeScraperController)(nil)
	_ scrapercontroller.LogsScraperRegistry    = (*timeScraperController)(nil)
)

func newTimeScraperController(cfg *Config, logger *zap.Logger) *timeScraperController {
	if logger == nil {
		logger = zap.NewNop()
	}
	tc := &timeScraperController{
		cfg:     cfg,
		logger:  logger,
		metrics: make(map[uint64]metricsRegistration),
		logs:    make(map[uint64]logsRegistration),
		done:    make(chan struct{}),
	}
	tc.StartFunc = tc.start
	tc.ShutdownFunc = tc.shutdown
	return tc
}

func (tc *timeScraperController) start(_ context.Context, _ component.Host) error {
	tc.wg.Add(1)
	go tc.run()
	return nil
}

func (tc *timeScraperController) shutdown(_ context.Context) error {
	close(tc.done)
	tc.wg.Wait()
	return nil
}

func (tc *timeScraperController) run() {
	defer tc.wg.Done()

	// Initial delay.
	if tc.cfg.InitialDelay > 0 {
		timer := time.NewTimer(tc.cfg.InitialDelay)
		select {
		case <-timer.C:
		case <-tc.done:
			timer.Stop()
			return
		}
	}

	ticker := time.NewTicker(tc.cfg.CollectionInterval)
	defer ticker.Stop()

	// Trigger once on start, then on each tick.
	tc.trigger()
	for {
		select {
		case <-ticker.C:
			tc.trigger()
		case <-tc.done:
			return
		}
	}
}

func (tc *timeScraperController) trigger() {
	// Snapshot registered scrapers without holding the lock while scraping.
	tc.mu.RLock()
	metrics := make([]metricsRegistration, 0, len(tc.metrics))
	for _, s := range tc.metrics {
		metrics = append(metrics, s)
	}
	logs := make([]logsRegistration, 0, len(tc.logs))
	for _, s := range tc.logs {
		logs = append(logs, s)
	}
	tc.mu.RUnlock()

	ctx := context.Background()
	cancel := func() {}
	if tc.cfg.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, tc.cfg.Timeout)
	}
	defer cancel()

	// TODO run concurrently?
	for _, reg := range metrics {
		md, err := reg.scraper.ScrapeMetrics(ctx)
		if err != nil {
			tc.logger.Debug("Failed to scrape metrics", zap.Error(err))
			continue
		}
		if err := reg.consumer.ConsumeMetrics(ctx, md); err != nil {
			tc.logger.Debug("Failed to consume metrics", zap.Error(err))
		}
	}
	for _, reg := range logs {
		ld, err := reg.scraper.ScrapeLogs(ctx)
		if err != nil {
			tc.logger.Debug("Failed to scrape logs", zap.Error(err))
			continue
		}
		if err := reg.consumer.ConsumeLogs(ctx, ld); err != nil {
			tc.logger.Debug("Failed to consume logs", zap.Error(err))
		}
	}
}

func (tc *timeScraperController) RegisterMetrics(s scraper.Metrics, next consumer.Metrics) (component.ShutdownFunc, error) {
	id := tc.idGen.Add(1)
	tc.mu.Lock()
	tc.metrics[id] = metricsRegistration{scraper: s, consumer: next}
	tc.mu.Unlock()
	return tc.unregister(id), nil
}

func (tc *timeScraperController) RegisterLogs(s scraper.Logs, next consumer.Logs) (component.ShutdownFunc, error) {
	id := tc.idGen.Add(1)
	tc.mu.Lock()
	tc.logs[id] = logsRegistration{scraper: s, consumer: next}
	tc.mu.Unlock()
	return tc.unregister(id), nil
}

func (tc *timeScraperController) unregister(id uint64) component.ShutdownFunc {
	var once sync.Once
	return func(context.Context) error {
		// TODO make sure that ongoing scrapes are canceled and
		// completed before returning.
		once.Do(func() {
			tc.mu.Lock()
			delete(tc.metrics, id)
			delete(tc.logs, id)
			tc.mu.Unlock()
		})
		return nil
	}
}
