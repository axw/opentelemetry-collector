// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package httpscrapercontroller

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/scraper"
)

func TestHTTPScraperController_TriggerLogs(t *testing.T) {
	cfg := createDefaultConfig()
	cfg.NetAddr.Endpoint = "localhost:0"
	cfg.Path = "/scrape"

	hc := newHTTPScraperController(cfg, componenttest.NewNopTelemetrySettings(), nil)
	require.NoError(t, hc.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, hc.Shutdown(context.Background())) })

	sink := new(consumertest.LogsSink)

	logsScraper, err := scraper.NewLogs(func(context.Context) (plog.Logs, error) {
		ld := plog.NewLogs()
		ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("hello")
		return ld, nil
	})
	require.NoError(t, err)

	unregister, err := hc.RegisterLogs(logsScraper, sink)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, unregister.Shutdown(context.Background())) })

	url := "http://" + hc.Addr() + cfg.Path
	require.Eventually(t, func() bool {
		resp, e := http.Get(url)
		if e != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		ld := sink.AllLogs()
		return len(ld) > 0 && ld[len(ld)-1].LogRecordCount() == 1
	}, 2*time.Second, 10*time.Millisecond)
}

