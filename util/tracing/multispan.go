package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
)

// MultiSpan allows shared tracing to multiple spans.
// TODO: This is a temporary solution and doesn't really support shared tracing yet. Instead the first always wins.

type MultiSpan struct {
	embedded.Span

	spans []trace.Span
	ended bool // XXX: mu
}

var _ trace.Span = &MultiSpan{}

func NewMultiSpan() *MultiSpan {
	return &MultiSpan{}
}

func (ms *MultiSpan) Add(span trace.Span) {
	ms.spans = append(ms.spans, span)
}

func (ms *MultiSpan) getForTrace(traceID trace.TraceID) trace.Span {
	for _, span := range ms.spans {
		if mspan, ok := span.(*MultiSpan); ok {
			result := mspan.getForTrace(traceID)
			if result != nil {
				return result
			}
		} else if span.SpanContext().TraceID() == traceID {
			return span
		}
	}
	return nil
}

func (ms *MultiSpan) End(options ...trace.SpanEndOption) {
	// ms.ended = true
	for _, span := range ms.spans {
		span.End(options...)
	}
}

func (ms *MultiSpan) AddEvent(name string, options ...trace.EventOption) {
	for _, span := range ms.spans {
		span.AddEvent(name, options...)
	}
}

func (ms *MultiSpan) AddLink(link trace.Link) {
	for _, span := range ms.spans {
		span.AddLink(link)
	}
}

func (ms *MultiSpan) IsRecording() bool {
	return !ms.ended
	// if len(ms.spans) == 0 {
	// 	return true
	// }
	// return ms.spans[0].IsRecording()
}

func (ms *MultiSpan) RecordError(err error, options ...trace.EventOption) {
	for _, span := range ms.spans {
		span.RecordError(err, options...)
	}
}

func (ms *MultiSpan) SpanContext() trace.SpanContext {
	if len(ms.spans) == 0 {
		return trace.SpanContext{}
	}
	return ms.spans[0].SpanContext()
}

func (ms *MultiSpan) SetStatus(code codes.Code, description string) {
	for _, span := range ms.spans {
		span.SetStatus(code, description)
	}
}

func (ms *MultiSpan) SetName(name string) {
	for _, span := range ms.spans {
		span.SetName(name)
	}
}

func (ms *MultiSpan) SetAttributes(kv ...attribute.KeyValue) {
	for _, span := range ms.spans {
		span.SetAttributes(kv...)
	}
}

func (ms *MultiSpan) TracerProvider() trace.TracerProvider {
	// if len(ms.spans) == 0 {
	// 	return nil
	// }
	// return ms.spans[0].TracerProvider()

	// providers := make([]trace.TracerProvider, len(ms.spans))
	// for i, span := range ms.spans {
	// 	providers[i] = span.TracerProvider()
	// }
	// return &multiTracerProvider{providers: providers, spans: append([]trace.Span{}, ms.spans...)}
	return &multiTracerProvider{}
}

type multiTracerProvider struct {
	embedded.TracerProvider

	// providers []trace.TracerProvider
	// spans     []trace.Span
}

var _ trace.TracerProvider = &multiTracerProvider{}

// func (mtp *multiTracerProvider) Tracer(name string, options ...trace.TracerOption) trace.Tracer {
// 	tracers := make([]trace.Tracer, len(mtp.providers))
// 	for i, provider := range mtp.providers {
// 		tracers[i] = provider.Tracer(name, options...)
// 	}
// 	return &multiTracer{tracers: tracers, spans: mtp.spans}
// }

func (mtp *multiTracerProvider) Tracer(name string, options ...trace.TracerOption) trace.Tracer {
	return &multiTracer{name: name, options: options}
}

type multiTracer struct {
	embedded.Tracer

	name    string
	options []trace.TracerOption
}

// var _ trace.Tracer = &multiTracer{}

// func (mt *multiTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
// 	parent := trace.SpanFromContext(ctx)
// 	if multiParent, ok := parent.(*MultiSpan); ok {
// 		spans := make([]trace.Span, len(mt.tracers))
// 		for i, tracer := range mt.tracers {
// 			parent := multiParent.getForTrace(mt.spans[i].SpanContext().TraceID())
// 			if parent == nil {
// 				panic(parent)
// 			}
// 			_, span := tracer.Start(trace.ContextWithSpan(ctx, parent), spanName, opts...)
// 			spans[i] = span
// 		}
// 		span := &MultiSpan{spans: spans}
// 		return trace.ContextWithSpan(ctx, span), span
// 	} else {

// 	}
// }

func (mt *multiTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	parent := trace.SpanFromContext(ctx)
	if parent == nil {
		panic("no parent span found")
	} else if multiParent, ok := parent.(*MultiSpan); ok {
		spans := make([]trace.Span, len(multiParent.spans))
		for i, parent := range multiParent.spans {
			tracer := parent.TracerProvider().Tracer(mt.name, mt.options...)
			_, span := tracer.Start(trace.ContextWithSpan(ctx, parent), spanName, opts...)
			spans[i] = span
		}
		span := &MultiSpan{spans: spans}
		return trace.ContextWithSpan(ctx, span), span
	} else {
		// XXX: strange, this is kinda not right
		// I think this is an edge case we should fix?
		tracer := parent.TracerProvider().Tracer(mt.name, mt.options...)
		return tracer.Start(trace.ContextWithSpan(ctx, parent), spanName, opts...)
	}
}
