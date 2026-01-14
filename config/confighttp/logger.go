// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package confighttp // import "go.opentelemetry.io/collector/config/confighttp"

import (
	"net/http"

	"go.opentelemetry.io/collector/component"
)

type loggerHandler struct {
	next     http.Handler
	settings component.TelemetrySettings
}

func (h *loggerHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	logger := h.settings.ContextLogger(ctx).WithLazy(component.TraceContextFields(ctx)...)
	ctx = component.ContextWithLogger(ctx, logger)
	h.next.ServeHTTP(w, req.WithContext(ctx))
}
