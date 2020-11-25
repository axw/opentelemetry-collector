// Copyright 2020 The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package simple

import (
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/collector/consumer/pdata"
)

// Metrics facilitates building pdata.Metrics in receivers.  It is meant
// to be much easier and more fluent than than using pdata.Metrics directly.
// All of the exported methods on it return the same instance of Metrics
// as a return value, allowing you to chain method calls easily, similar to the
// Java builder pattern.
//
// All of the public fields in this structure are meant to be set before the
// first data point is added, and should not be changed afterwards.
//
// The Metrics is designed for cases where receivers are generating
// metrics from scratch, where generally you will have a single datapoint per
// metric/label combination.
//
// One restriction this helper imposes is that a particular metric name must
// only be used with a single data type for all instances derived from a base
// helper, including the base instance.  This restriction greatly simplifies
// the logic to reuse metrics for multiple datapoints and it is generally
// easier for backends to not have to deal with conflicting types anyway.
//
// It is NOT thread-safe, so you should use an external mutex if using it from
// multiple goroutines.
type Metrics struct {
	// REQUIRED. A Metrics object that has been created with
	// `pdata.NewMetrics()`.  This is required to be set on the builder.  All
	// metrics added will go into this immediately upon invocation of Add*
	// methods.  Do not change this once initially set.
	pdata.Metrics

	// MetricFactoriesByName is an optional map of metric factories that will
	// be created with the appropriate name, description, and type field.  This
	// is intended to be used with the metadata code generation modules but can
	// be used apart from that just as well.  The returned metrics are expected
	// to be initialized.
	MetricFactoriesByName map[string]func() pdata.Metric

	// If set, this instrumentation library name will be used for all metrics
	// generated by this builder.  This is meant to be set once at builder
	// creation and not changed later.
	InstrumentationLibraryName string
	// If set, this instrumentation library version will be used for all
	// metrics generated by this builder.  This is meant to be set once at
	// builder creation and not changed later.
	InstrumentationLibraryVersion string
	// These attributes will be added to the Resource object on all
	// ResourceMetrics instances created by the builder.  This is meant to be
	// set once at builder creation and not changed later.
	ResourceAttributes map[string]string
	// This time will be used as the Timestamp for all metrics generated.  It
	// can be updated with a new timestamp at any time.
	Timestamp time.Time
	// A set of labels that will be applied to all datapoints emitted by the
	// builder.
	Labels map[string]string

	resourceMetricIdx **int
	metricIdxByName   map[string]int
}

func (mb *Metrics) ensureInit() {
	if mb.metricIdxByName == nil {
		mb.metricIdxByName = map[string]int{}
	}
	if mb.resourceMetricIdx == nil {
		var ip *int
		mb.resourceMetricIdx = &ip
	}
}

// Clone the MetricBuilder.  All of the maps copied will be deeply copied.
func (mb *Metrics) clone() *Metrics {
	mb.ensureInit()

	return &Metrics{
		Metrics:                       mb.Metrics,
		MetricFactoriesByName:         mb.MetricFactoriesByName,
		InstrumentationLibraryName:    mb.InstrumentationLibraryName,
		InstrumentationLibraryVersion: mb.InstrumentationLibraryVersion,
		ResourceAttributes:            cloneStringMap(mb.ResourceAttributes),
		Timestamp:                     mb.Timestamp,
		Labels:                        cloneStringMap(mb.Labels),
		resourceMetricIdx:             mb.resourceMetricIdx,
		metricIdxByName:               mb.metricIdxByName,
	}
}

// WithLabels returns a new, independent builder with additional labels. These
// labels will be combined with the Labels that can be set on the struct.
// All subsequent calls to create metrics will create metrics that use these
// labels.  The input map's entries are copied so the map can be mutated freely
// by the caller afterwards without affecting the builder.
func (mb *Metrics) WithLabels(l map[string]string) *Metrics {
	out := mb.clone()

	for k, v := range l {
		out.Labels[k] = v
	}

	return out
}

// AsSafeBuilder returns an instance of this builder wrapped in
// SafeMetrics that ensures all of the public methods on this instance
// will be thread-safe between goroutines.  You must explicitly type these
// instances as SafeMetrics.
func (mb Metrics) AsSafe() *SafeMetrics {
	return &SafeMetrics{
		Metrics: &mb,
		Mutex:   &sync.Mutex{},
	}
}

func (mb *Metrics) AddGaugeDataPoint(name string, metricValue int64) *Metrics {
	typ := pdata.MetricDataTypeIntGauge
	mb.addDataPoint(name, typ, metricValue)
	return mb
}

func (mb *Metrics) AddDGaugeDataPoint(name string, metricValue float64) *Metrics {
	typ := pdata.MetricDataTypeDoubleGauge
	mb.addDataPoint(name, typ, metricValue)
	return mb
}

func (mb *Metrics) AddSumDataPoint(name string, metricValue int64) *Metrics {
	typ := pdata.MetricDataTypeIntSum
	mb.addDataPoint(name, typ, metricValue)
	return mb
}

func (mb *Metrics) AddDSumDataPoint(name string, metricValue float64) *Metrics {
	typ := pdata.MetricDataTypeDoubleSum
	mb.addDataPoint(name, typ, metricValue)
	return mb
}

func (mb *Metrics) AddHistogramRawDataPoint(name string, hist pdata.IntHistogramDataPoint) *Metrics {
	mb.addDataPoint(name, pdata.MetricDataTypeIntHistogram, hist)
	return mb
}

func (mb *Metrics) AddDHistogramRawDataPoint(name string, hist pdata.DoubleHistogramDataPoint) *Metrics {
	mb.addDataPoint(name, pdata.MetricDataTypeDoubleHistogram, hist)
	return mb
}

func (mb *Metrics) getMetricsSlice() pdata.MetricSlice {
	rms := mb.Metrics.ResourceMetrics()
	if mb.resourceMetricIdx != nil && *mb.resourceMetricIdx != nil {
		return rms.At(**mb.resourceMetricIdx).InstrumentationLibraryMetrics().At(0).Metrics()
	}

	rmsLen := rms.Len()
	rms.Resize(rmsLen + 1)
	rm := rms.At(rmsLen)
	rm.InitEmpty()

	res := rm.Resource()
	for k, v := range mb.ResourceAttributes {
		res.Attributes().Insert(k, pdata.NewAttributeValueString(v))
	}

	ilms := rm.InstrumentationLibraryMetrics()
	ilms.Resize(1)
	ilm := ilms.At(0)
	ilm.InitEmpty()

	il := ilm.InstrumentationLibrary()
	il.SetName(mb.InstrumentationLibraryName)
	il.SetVersion(mb.InstrumentationLibraryVersion)

	*mb.resourceMetricIdx = &rmsLen

	return ilm.Metrics()
}

func (mb *Metrics) getOrCreateMetric(name string, typ pdata.MetricDataType) pdata.Metric {
	mb.ensureInit()

	metricSlice := mb.getMetricsSlice()

	idx, ok := mb.metricIdxByName[name]
	if ok {
		return metricSlice.At(idx)
	}

	var metric pdata.Metric
	if fac, ok := mb.MetricFactoriesByName[name]; ok {
		metric = fac()
	} else {
		metric = pdata.NewMetric()
		metric.InitEmpty()

		metric.SetName(name)
		metric.SetDataType(typ)
	}

	metricSlice.Append(metric)

	mb.metricIdxByName[name] = metricSlice.Len() - 1
	return metric
}

func (mb *Metrics) addDataPoint(name string, typ pdata.MetricDataType, val interface{}) {
	metric := mb.getOrCreateMetric(name, typ)

	// This protects against reusing the same metric name with different types.
	if metric.DataType() != typ {
		panic(fmt.Errorf("mismatched metric data types for metric %q: %q vs %q", metric.Name(), metric.DataType(), typ))
	}

	tsNano := pdata.TimestampUnixNano(mb.Timestamp.UnixNano())

	switch typ {
	case pdata.MetricDataTypeIntGauge:
		m := metric.IntGauge()
		dps := m.DataPoints()
		dp := pdata.NewIntDataPoint()
		dp.InitEmpty()
		dp.LabelsMap().InitFromMap(mb.Labels)
		dp.SetValue(val.(int64))
		dp.SetTimestamp(tsNano)
		dps.Append(dp)

	case pdata.MetricDataTypeIntSum:
		m := metric.IntSum()
		dps := m.DataPoints()
		dp := pdata.NewIntDataPoint()
		dp.InitEmpty()
		dp.LabelsMap().InitFromMap(mb.Labels)
		dp.SetValue(val.(int64))
		dp.SetTimestamp(tsNano)
		dps.Append(dp)

	case pdata.MetricDataTypeDoubleGauge:
		m := metric.DoubleGauge()
		dps := m.DataPoints()
		dp := pdata.NewDoubleDataPoint()
		dp.InitEmpty()
		dp.LabelsMap().InitFromMap(mb.Labels)
		dp.SetValue(val.(float64))
		dp.SetTimestamp(tsNano)
		dps.Append(dp)

	case pdata.MetricDataTypeDoubleSum:
		m := metric.DoubleSum()
		dps := m.DataPoints()
		dp := pdata.NewDoubleDataPoint()
		dp.InitEmpty()
		dp.LabelsMap().InitFromMap(mb.Labels)
		dp.SetValue(val.(float64))
		dp.SetTimestamp(tsNano)
		dps.Append(dp)

	case pdata.MetricDataTypeIntHistogram:
		m := metric.IntHistogram()
		dps := m.DataPoints()
		dp := pdata.NewIntHistogramDataPoint()
		dp.InitEmpty()
		dp.LabelsMap().InitFromMap(mb.Labels)
		val.(pdata.IntHistogramDataPoint).CopyTo(dp)
		dp.SetTimestamp(tsNano)
		dps.Append(dp)

	case pdata.MetricDataTypeDoubleHistogram:
		m := metric.DoubleHistogram()
		dps := m.DataPoints()
		dp := pdata.NewDoubleHistogramDataPoint()
		dp.InitEmpty()
		dp.LabelsMap().InitFromMap(mb.Labels)
		val.(pdata.DoubleHistogramDataPoint).CopyTo(dp)
		dp.SetTimestamp(tsNano)
		dps.Append(dp)

	default:
		panic("invalid metric type: " + typ.String())
	}
}

func cloneStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// SafeMetrics is a wrapper for Metrics that ensures the wrapped
// instance can be used safely across goroutines.  It is meant to be created
// from the AsSafeBuilder on Metrics.
type SafeMetrics struct {
	*sync.Mutex
	*Metrics
}

func (mb *SafeMetrics) WithLabels(l map[string]string) *SafeMetrics {
	mb.Lock()
	defer mb.Unlock()

	return &SafeMetrics{
		Metrics: mb.Metrics.WithLabels(l),
		Mutex:   mb.Mutex,
	}
}

func (mb *SafeMetrics) AddGaugeDataPoint(name string, metricValue int64) *SafeMetrics {
	mb.Lock()
	mb.Metrics.AddGaugeDataPoint(name, metricValue)
	mb.Unlock()
	return mb
}

func (mb *SafeMetrics) AddDGaugeDataPoint(name string, metricValue float64) *SafeMetrics {
	mb.Lock()
	mb.Metrics.AddDGaugeDataPoint(name, metricValue)
	mb.Unlock()
	return mb
}

func (mb *SafeMetrics) AddSumDataPoint(name string, metricValue int64) *SafeMetrics {
	mb.Lock()
	mb.Metrics.AddSumDataPoint(name, metricValue)
	mb.Unlock()
	return mb
}

func (mb *SafeMetrics) AddDSumDataPoint(name string, metricValue float64) *SafeMetrics {
	mb.Lock()
	mb.Metrics.AddDSumDataPoint(name, metricValue)
	mb.Unlock()
	return mb
}

func (mb *SafeMetrics) AddHistogramRawDataPoint(name string, hist pdata.IntHistogramDataPoint) *SafeMetrics {
	mb.Lock()
	mb.Metrics.AddHistogramRawDataPoint(name, hist)
	mb.Unlock()
	return mb
}

func (mb *SafeMetrics) AddDHistogramRawDataPoint(name string, hist pdata.DoubleHistogramDataPoint) *SafeMetrics {
	mb.Lock()
	mb.Metrics.AddDHistogramRawDataPoint(name, hist)
	mb.Unlock()
	return mb
}
