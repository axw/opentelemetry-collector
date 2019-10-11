// Copyright 2019, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package elasticexporter

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"contrib.go.opencensus.io/exporter/ocagent"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"go.elastic.co/apm"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector/config/configmodels"
	"github.com/open-telemetry/opentelemetry-collector/consumer/consumerdata"
	"github.com/open-telemetry/opentelemetry-collector/exporter"
	"github.com/open-telemetry/opentelemetry-collector/exporter/exporterhelper"
)

type elasticExporter struct {
	// map of service names to tracers
	tracers map[string]*apm.Tracer
}

// NewTraceExporter creates an Elastic APM trace exporter.
func NewTraceExporter(logger *zap.Logger, config configmodels.Exporter, opts ...ocagent.ExporterOption) (exporter.TraceExporter, error) {
	e, err := createElasticExporter(logger, opts...)
	if err != nil {
		return nil, err
	}
	return exporterhelper.NewTraceExporter(
		config,
		e.PushTraceData,
		exporterhelper.WithTracing(true),
		exporterhelper.WithMetrics(true),
		exporterhelper.WithShutdown(e.Shutdown),
	)
}

// createElasticExporter takes ocagent exporter options and create an OC exporter
func createElasticExporter(logger *zap.Logger, opts ...ocagent.ExporterOption) (*elasticExporter, error) {
	return &elasticExporter{tracers: make(map[string]*apm.Tracer)}, nil
}

func (e *elasticExporter) Shutdown() error {
	for _, tracer := range e.tracers {
		tracer.Close()
	}
	return nil
}

func (e *elasticExporter) PushTraceData(ctx context.Context, td consumerdata.TraceData) (int, error) {
	serviceName := td.Node.ServiceInfo.Name
	tracer := e.tracers[serviceName]
	if tracer == nil {
		var err error
		tracer, err = apm.NewTracer(serviceName, "")
		if err != nil {
			return len(td.Spans), err
		}
		e.tracers[serviceName] = tracer
	}

	// TODO(axw) td.Node -> metadata
	//   td.Node.Identifier.HostName -> system.hostname
	//   td.Node.Identifier.Pid -> process.pid
	//   td.Node.Attributes["ip"] -> host IP
	//   td.Node.LibraryInfo.Language -> agent.language

	// All transactions/spans are sampled.
	var traceOptions apm.TraceOptions
	traceOptions = traceOptions.WithRecorded(true)

	for _, span := range td.Spans {
		var parentSpanID apm.SpanID
		root := len(span.ParentSpanId) == 0
		if !root {
			copy(parentSpanID[:], span.ParentSpanId)
		}

		var traceID apm.TraceID
		copy(traceID[:], span.TraceId)

		var spanID apm.SpanID
		copy(spanID[:], span.SpanId)

		startTime := time.Unix(span.StartTime.Seconds, int64(span.StartTime.Nanos))
		endTime := time.Unix(span.EndTime.Seconds, int64(span.EndTime.Nanos))
		// TODO(axw) span.StackTrace
		// TODO(axw) span.TimeEvents -> marks
		// TODO(axw) span.ChildSpanCount -> span_count.started
		// TODO(axw) span.Resource, span.Attributes -> context
		// TODO(axw) span.Status -> result

		fmt.Printf("%s: %s (%s)\n", td.Node.ServiceInfo.Name, span.Name.Value, span.Kind)
		if attrs := span.Attributes.GetAttributeMap(); len(attrs) > 0 {
			fmt.Println(".. span attributes:")
			for k, v := range attrs {
				fmt.Printf(".... %s: %v\n", k, v.Value)
			}
		}
		if root || span.Kind == tracepb.Span_SERVER {
			tx := tracer.StartTransactionOptions(
				span.Name.Value,
				"request", // TODO(axw) based on attributes
				apm.TransactionOptions{
					Start:         startTime,
					TransactionID: spanID,
					TraceContext: apm.TraceContext{
						Trace:   traceID,
						Span:    parentSpanID,
						Options: traceOptions,
					},
				},
			)
			var httpMethod, httpURL string
			for k, v := range span.Attributes.GetAttributeMap() {
				switch v := v.Value.(type) {
				case *tracepb.AttributeValue_BoolValue:
					tx.Context.SetLabel(k, v.BoolValue)
				case *tracepb.AttributeValue_DoubleValue:
					tx.Context.SetLabel(k, v.DoubleValue)
				case *tracepb.AttributeValue_IntValue:
					switch k {
					case "http.status_code":
						tx.Context.SetHTTPStatusCode(int(v.IntValue))
					default:
						tx.Context.SetLabel(k, v.IntValue)
					}
				case *tracepb.AttributeValue_StringValue:
					switch k {
					case "span.kind": // filter out
					case "http.method":
						httpMethod = v.StringValue.Value
					case "http.url":
						httpURL = v.StringValue.Value
					default:
						tx.Context.SetLabel(k, v.StringValue.Value)
					}
				}
			}
			if httpURL != "" {
				url, err := url.Parse(httpURL)
				if err == nil {
					if url.Scheme == "" {
						url.Scheme = "http"
					}
					if url.Host == "" {
						url.Host = td.Node.Identifier.HostName
					}
				}
				tx.Context.SetHTTPRequest(&http.Request{
					// NOTE(axw) the agent assumes it always knows the
					// version, but if we do this in the server then
					// we would just leave the HTTP version out.
					ProtoMajor: 1,
					ProtoMinor: 1,
					Method:     httpMethod,
					URL:        url,
				})
			}
			tx.Duration = endTime.Sub(startTime)
			tx.End()
		} else {
			// BUG(axw) if the parent is another span, then the transaction
			// ID here will be invalid. We should instead stop specifying the
			// transaction ID (requires APM Server 7.1+)
			transactionID := parentSpanID
			apmSpan := tracer.StartSpan(
				span.Name.Value,
				"request", // TODO(axw) based on attributes
				transactionID,
				apm.SpanOptions{
					Start:  startTime,
					SpanID: spanID,
					Parent: apm.TraceContext{
						Trace:   traceID,
						Span:    parentSpanID,
						Options: traceOptions,
					},
				},
			)
			var httpStatusCode int
			var httpURL string
			for k, v := range span.Attributes.GetAttributeMap() {
				switch v := v.Value.(type) {
				case *tracepb.AttributeValue_BoolValue:
					apmSpan.Context.SetLabel(k, v.BoolValue)
				case *tracepb.AttributeValue_DoubleValue:
					apmSpan.Context.SetLabel(k, v.DoubleValue)
				case *tracepb.AttributeValue_IntValue:
					switch k {
					case "http.status_code":
						httpStatusCode = int(v.IntValue)
					default:
						apmSpan.Context.SetLabel(k, v.IntValue)
					}
				case *tracepb.AttributeValue_StringValue:
					switch k {
					case "span.kind": // filter out
					case "http.url":
						httpURL = v.StringValue.Value
					case "sql.query":
						// NOTE(axw) in theory we could override
						// the span name here with our own method
						// based on the SQL query.
						apmSpan.Context.SetDatabase(apm.DatabaseSpanContext{
							Type:      "sql",
							Statement: v.StringValue.Value,
						})
					default:
						apmSpan.Context.SetLabel(k, v.StringValue.Value)
					}
				}
				if httpURL != "" {
					url, err := url.Parse(httpURL)
					if err != nil {
						if strings.Index(httpURL, "://") == -1 {
							url, _ = url.Parse("http://" + httpURL)
						}
					}
					// TODO(axw) the Go agent has a bug where if the status code is
					// set, but the URL is not, then it will panic. Fortunately this
					// is never the case in our instrumentation, but we should fix it
					// and then we can set the status code regardless.
					if url != nil {
						apmSpan.Context.SetHTTPStatusCode(httpStatusCode)
						apmSpan.Context.SetHTTPRequest(&http.Request{URL: url})
					} else {
						apmSpan.Context.SetLabel("http.status_code", httpStatusCode)
						apmSpan.Context.SetLabel("http.url", httpURL)
					}
				}
			}
			apmSpan.Duration = endTime.Sub(startTime)
			apmSpan.End()
		}
	}
	return 0, nil
}

/*
// NewMetricsExporter creates an Elastic APM metrics exporter.
func NewMetricsExporter(logger *zap.Logger, config configmodels.Exporter, opts ...ocagent.ExporterOption) (exporter.MetricsExporter, error) {
	oce, err := createElasticExporter(logger, config, opts...)
	if err != nil {
		return nil, err
	}
	oexp, err := exporterhelper.NewMetricsExporter(
		config,
		oce.PushMetricsData,
		exporterhelper.WithTracing(true),
		exporterhelper.WithMetrics(true),
		exporterhelper.WithShutdown(oce.Shutdown))
	if err != nil {
		return nil, err
	}

	return oexp, nil
}

func (oce *elasticExporter) PushMetricsData(ctx context.Context, md consumerdata.MetricsData) (int, error) {
	// Get first available exporter.
	exporter, ok := <-oce.exporters
	if !ok {
		err := &elasticExporterError{
			code: errAlreadyStopped,
			msg:  fmt.Sprintf("Elastic APM exporter was already stopped."),
		}
		return exporterhelper.NumTimeSeries(md), err
	}

	req := &agentmetricspb.ExportMetricsServiceRequest{
		Metrics:  md.Metrics,
		Resource: md.Resource,
		Node:     md.Node,
	}
	err := exporter.ExportMetricsServiceRequest(req)
	oce.exporters <- exporter
	if err != nil {
		return exporterhelper.NumTimeSeries(md), err
	}
	return 0, nil
}
*/
