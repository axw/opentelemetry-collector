// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package componentattribute // import "go.opentelemetry.io/collector/internal/telemetry/componentattribute"

import (
	"slices"

	"go.opentelemetry.io/otel/attribute"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type tracerProviderWithAttributes struct {
	trace.TracerProvider
	attrs []attribute.KeyValue
}

// racerProviderWithAttributesRegisterable is an extension of tracerProviderWithAttributes
// that also exposes the sdk/trace.TracerProvider type's RegisterSpanProcessor and
// UnregisterSpanProcessor methods, as used by zpages.
type tracerProviderWithAttributesRegisterable struct {
	tracerProviderWithAttributes
	registerableTracerProvider
}

type registerableTracerProvider interface {
	RegisterSpanProcessor(sdkTrace.SpanProcessor)
	UnregisterSpanProcessor(sdkTrace.SpanProcessor)
}

// TracerProviderWithAttributes creates a TracerProvider with a new set of injected instrumentation scope attributes.
func TracerProviderWithAttributes(tp trace.TracerProvider, attrs attribute.Set) trace.TracerProvider {
	switch tp := tp.(type) {
	case tracerProviderWithAttributes:
		tp.attrs = attrs.ToSlice()
		return tp
	case tracerProviderWithAttributesRegisterable:
		tp.attrs = attrs.ToSlice()
		return tp
	default:
		// Not yet wrapped.
		tpwa := tracerProviderWithAttributes{
			TracerProvider: tp,
			attrs:          attrs.ToSlice(),
		}
		if r, ok := tp.(registerableTracerProvider); ok {
			return tracerProviderWithAttributesRegisterable{
				tracerProviderWithAttributes: tpwa,
				registerableTracerProvider:   r,
			}
		}
		return tpwa
	}
}

func tracerWithAttributes(tp trace.TracerProvider, attrs []attribute.KeyValue, name string, opts ...trace.TracerOption) trace.Tracer {
	conf := trace.NewTracerConfig(opts...)
	attrSet := conf.InstrumentationAttributes()
	// prepend our attributes so they can be overwritten
	newAttrs := append(slices.Clone(attrs), attrSet.ToSlice()...)
	// append our attribute set option to overwrite the old one
	opts = append(opts, trace.WithInstrumentationAttributes(newAttrs...))
	return tp.Tracer(name, opts...)
}

func (tpwa tracerProviderWithAttributes) Tracer(name string, options ...trace.TracerOption) trace.Tracer {
	return tracerWithAttributes(tpwa.TracerProvider, tpwa.attrs, name, options...)
}
