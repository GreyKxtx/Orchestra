// Package telemetry exports what a turn did as OpenTelemetry traces: the
// agent, the subagents it started, their model calls and their tool calls,
// as spans named by the GenAI semantic conventions (gen_ai.*), sent over
// OTLP/HTTP in JSON to a collector, Jaeger, Tempo or whatever listens on
// /v1/traces.
//
// The wire encoding is written here rather than taken from the OpenTelemetry
// SDK: one JSON document per turn is all the exporter sends, and the SDK's
// pipeline (processors, protobuf, gRPC) would bring in more code than the
// rest of the binary's dependencies together. The document follows the
// OTLP JSON mapping: ids as hex strings, 64-bit integers as decimal
// strings, attribute values as one-key unions.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// otlpPayload is ExportTraceServiceRequest in its JSON mapping.
type otlpPayload struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource    `json:"resource"`
	ScopeSpans []otlpScopeSpan `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}

type otlpScopeSpan struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// Span kinds, as the protobuf enum numbers them.
const (
	spanKindInternal = 1
	spanKindClient   = 3
)

// Status codes: unset, ok, error.
const (
	statusUnset = 0
	statusOK    = 1
	statusError = 2
)

type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId,omitempty"`
	Name              string          `json:"name"`
	Kind              int             `json:"kind"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano"`
	Attributes        []otlpAttribute `json:"attributes,omitempty"`
	Status            otlpStatus      `json:"status"`
}

type otlpStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type otlpAttribute struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

// otlpValue is AnyValue: exactly one of the fields is set.
type otlpValue struct {
	StringValue *string         `json:"stringValue,omitempty"`
	IntValue    *string         `json:"intValue,omitempty"`
	ArrayValue  *otlpArrayValue `json:"arrayValue,omitempty"`
}

type otlpArrayValue struct {
	Values []otlpValue `json:"values"`
}

func attrString(key, v string) otlpAttribute {
	return otlpAttribute{Key: key, Value: otlpValue{StringValue: &v}}
}

func attrInt(key string, v int64) otlpAttribute {
	s := strconv.FormatInt(v, 10)
	return otlpAttribute{Key: key, Value: otlpValue{IntValue: &s}}
}

func attrStrings(key string, vs []string) otlpAttribute {
	arr := &otlpArrayValue{Values: make([]otlpValue, 0, len(vs))}
	for _, v := range vs {
		v := v
		arr.Values = append(arr.Values, otlpValue{StringValue: &v})
	}
	return otlpAttribute{Key: key, Value: otlpValue{ArrayValue: arr}}
}

func unixNano(t time.Time) string {
	return strconv.FormatInt(t.UnixNano(), 10)
}

// postTimeout bounds one export: a collector that does not answer must not
// hold the core's shutdown.
const postTimeout = 10 * time.Second

// post sends payload to endpoint with headers. A 2xx is success; anything
// else is an error naming the status and the start of the body.
func post(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, payload otlpPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, postTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("collector answered %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}
