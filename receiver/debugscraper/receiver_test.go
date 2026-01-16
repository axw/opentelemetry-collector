// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package debugscraper

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func TestReceiverEmitsLogs(t *testing.T) {
	f := NewFactory()
	cfg := f.CreateDefaultConfig().(*Config)
	cfg.Message = "hello"
	cfg.Controller.CollectionInterval = time.Hour
	cfg.Controller.InitialDelay = 0

	sink := new(consumertest.LogsSink)
	r, err := f.CreateLogs(context.Background(), receivertest.NewNopSettings(typeStr), cfg, sink)
	require.NoError(t, err)

	require.NoError(t, r.Start(context.Background(), componenttest.NewNopHost()))

	require.Eventually(t, func() bool {
		logs := sink.AllLogs()
		if len(logs) != 1 {
			return false
		}
		return logs[0].LogRecordCount() == 1 &&
			logs[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Body().Str() == "hello"
	}, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, r.Shutdown(context.Background()))
}
