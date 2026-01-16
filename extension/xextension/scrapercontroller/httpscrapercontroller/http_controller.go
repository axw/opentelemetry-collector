// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package httpscrapercontroller // import "go.opentelemetry.io/collector/extension/xextension/scrapercontroller/httpscrapercontroller"

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

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

type httpController struct {
	component.StartFunc
	component.ShutdownFunc

	cfg       *Config
	telemetry component.TelemetrySettings
	logger    *zap.Logger

	idGen atomic.Uint64
	mu    sync.RWMutex

	metrics map[uint64]metricsRegistration
	logs    map[uint64]logsRegistration

	server *http.Server

	shutdownOnce sync.Once
}

var _ scrapercontroller.MetricsScraperRegistry = (*httpController)(nil)
var _ scrapercontroller.LogsScraperRegistry = (*httpController)(nil)

func newHTTPScraperController(cfg *Config, telemetry component.TelemetrySettings, logger *zap.Logger) *httpController {
	if logger == nil {
		logger = zap.NewNop()
	}
	hc := &httpController{
		cfg:       cfg,
		telemetry: telemetry,
		logger:    logger,
		metrics:   make(map[uint64]metricsRegistration),
		logs:      make(map[uint64]logsRegistration),
	}
	hc.StartFunc = hc.start
	hc.ShutdownFunc = hc.shutdown
	return hc
}

func (hc *httpController) start(ctx context.Context, host component.Host) error {
	srv, err := hc.cfg.ServerConfig.ToServer(
		ctx, host.GetExtensions(), hc.telemetry,
		http.HandlerFunc(hc.handleScrape),
	)
	if err != nil {
		return err
	}
	hc.server = srv

	ln, err := hc.cfg.ServerConfig.ToListener(ctx)
	if err != nil {
		return err
	}
	hc.logger.Info("listening for HTTP scraper controller requests", zap.String("address", ln.Addr().String()))

	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			hc.logger.Error("HTTP scraper controller server failed", zap.Error(serveErr))
		}
	}()
	return nil
}

func (hc *httpController) shutdown(ctx context.Context) error {
	var err error
	hc.shutdownOnce.Do(func() {
		if hc.server != nil {
			err = hc.server.Shutdown(ctx)
		}
	})
	return err
}

func (hc *httpController) RegisterMetrics(scr scraper.Metrics, next consumer.Metrics) (component.ShutdownFunc, error) {
	if scr == nil || next == nil {
		return func(context.Context) error { return nil }, nil
	}
	id := hc.idGen.Add(1)
	hc.mu.Lock()
	hc.metrics[id] = metricsRegistration{scraper: scr, consumer: next}
	hc.mu.Unlock()
	return hc.unregister(id, true), nil
}

func (hc *httpController) RegisterLogs(scr scraper.Logs, next consumer.Logs) (component.ShutdownFunc, error) {
	if scr == nil || next == nil {
		return func(context.Context) error { return nil }, nil
	}
	id := hc.idGen.Add(1)
	hc.mu.Lock()
	hc.logs[id] = logsRegistration{scraper: scr, consumer: next}
	hc.mu.Unlock()
	return hc.unregister(id, false), nil
}

func (hc *httpController) unregister(id uint64, isMetrics bool) component.ShutdownFunc {
	var once sync.Once
	return func(context.Context) error {
		once.Do(func() {
			hc.mu.Lock()
			if isMetrics {
				delete(hc.metrics, id)
			} else {
				delete(hc.logs, id)
			}
			hc.mu.Unlock()
		})
		return nil
	}
}

func (hc *httpController) handleScrape(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	cancel := func() {}
	if hc.cfg.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, hc.cfg.Timeout)
	}
	defer cancel()

	hc.mu.RLock()
	metricsRegs := make([]metricsRegistration, 0, len(hc.metrics))
	for _, v := range hc.metrics {
		metricsRegs = append(metricsRegs, v)
	}
	logsRegs := make([]logsRegistration, 0, len(hc.logs))
	for _, v := range hc.logs {
		logsRegs = append(logsRegs, v)
	}
	hc.mu.RUnlock()

	var failed int

	// NOTE: We intentionally keep this handler minimal; detailed error reporting can be added later.
	//
	// TODO consider parallelising the scrapes. Consider also grouping scrapers by next consumer,
	// and concatenating the data to reduce number of calls to next consumers. This would not be
	// useful if each scraper produced next to a batching processor.
	for _, reg := range metricsRegs {
		md, err := reg.scraper.ScrapeMetrics(ctx)
		if err != nil {
			failed++
			continue
		}
		consumeErr := reg.consumer.ConsumeMetrics(ctx, md)
		if consumeErr != nil {
			failed++
		}
	}

	for _, reg := range logsRegs {
		ld, err := reg.scraper.ScrapeLogs(ctx)
		if err != nil {
			failed++
			continue
		}
		consumeErr := reg.consumer.ConsumeLogs(ctx, ld)
		if consumeErr != nil {
			failed++
		}
	}

	if failed > 0 {
		http.Error(w, fmt.Sprintf("scrape failed: %d", failed), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
