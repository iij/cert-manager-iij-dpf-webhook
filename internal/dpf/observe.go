// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/observability"
)

// redacted は伏せた認証情報の代わりに置く値である。
const redacted = "[REDACTED]"

// observedTransport は DPF へ送った要求と DPF のエラー応答を記録し、呼び出しごとに span を作る
// (FR-088 / FR-088a / FR-091)。記録は要求の context が運ぶロガーへ出す。
//
// dpf-go は本 RoundTripper の外側で Authorization ヘッダを付ける。ヘッダは記録せず、その値
// (トークン) が本文に現れた場合は伏せる。エラー応答の本文も伏せたものに置き換えて返すため、
// 上位のエラーにもトークンは現れない (FR-012。認証情報の非出力が全文記録に優先する)。
type observedTransport struct {
	base http.RoundTripper
}

func (t *observedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	log := observability.Logger(ctx)
	token := bearerToken(req.Header.Get("Authorization"))
	redact := func(s string) string {
		if token == "" {
			return s
		}
		return strings.ReplaceAll(s, token, redacted) // domain-string: 認証情報を伏せる
	}

	body, err := requestBody(req)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req = req.Clone(ctx)
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	path := req.URL.Path
	zoneID := zoneIDOf(path)
	attrs := []any{"method", req.Method, "path", path, "zoneID", zoneID}
	if req.URL.RawQuery != "" {
		attrs = append(attrs, "query", req.URL.RawQuery)
	}
	if isZoneReplacement(req.Method, path) {
		// ゾーン全体の一括置換は本文がゾーンの規模に比例するため、総数のみを記録する。
		// 加除の差分は Apply が "DNS record change" として記録する (FR-088a)。
		attrs = append(attrs, "records", countRecords(body),
			"body", "(omitted: zone-wide replacement; see the DNS record change log)")
	} else if len(body) > 0 {
		attrs = append(attrs, "body", redact(string(body)))
	}
	log.Info("DPF API request", attrs...)

	ctx, span := observability.Tracer(ctx).Start(ctx, observability.SpanDPFPrefix+req.Method, trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("http.request.method", req.Method)))
	defer span.End()

	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		span.SetStatus(codes.Error, "transport error")
		log.Warn("DPF API request failed", "method", req.Method, "path", path, "zoneID", zoneID,
			"error", redact(err.Error()))
		return nil, err
	}
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	if resp.StatusCode < http.StatusBadRequest {
		return resp, nil
	}
	span.SetStatus(codes.Error, http.StatusText(resp.StatusCode))
	b, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	clean := []byte(redact(string(b)))
	resp.Body = io.NopCloser(bytes.NewReader(clean))
	resp.ContentLength = int64(len(clean))
	// エラー応答は切り詰めずに全文を記録する。問い合わせ用の request_id を失わない (FR-088)。
	errAttrs := []any{"method", req.Method, "path", path, "zoneID", zoneID, "status", resp.StatusCode,
		"body", string(clean)}
	if readErr != nil {
		errAttrs = append(errAttrs, "readError", readErr.Error())
	}
	log.Warn("DPF API error response", errAttrs...)
	return resp, nil
}

// requestBody は要求の本文を読む。元の要求の本文は消費しない。
func requestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	if req.GetBody != nil {
		rc, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		return io.ReadAll(rc)
	}
	b, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	return b, err
}

func bearerToken(h string) string {
	const prefix = "Bearer "
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return h
}

// zoneIDOf は URL のパスの "zones/<ID>" から ID を返す。
func zoneIDOf(path string) string {
	parts := strings.Split(path, "/") // domain-string: URL のパスの分割
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "zones" {
			return parts[i+1]
		}
	}
	return ""
}

// isZoneReplacement はゾーン全体の一括置換 (PATCH /zones/{id}/atomic_changes) かを返す。
func isZoneReplacement(method, path string) bool {
	return method == http.MethodPatch && zoneIDOf(path) != "" &&
		strings.HasSuffix(path, "/atomic_changes") // domain-string: URL のパスの判定
}

func countRecords(body []byte) int {
	var v struct {
		Records []json.RawMessage `json:"records"`
	}
	if json.Unmarshal(body, &v) != nil {
		return -1
	}
	return len(v.Records)
}
