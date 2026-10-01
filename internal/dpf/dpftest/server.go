// SPDX-License-Identifier: Apache-2.0

// Package dpftest は、テストのために DPF API を模擬する HTTP サーバを提供する。
//
// dpf-go の API の要求は具体型の API サービスを保持するため、インタフェースの差し替えでは
// 模擬できない。本パッケージは、本サービスと dpf-go の utils (排他・一括置き換え) が
// 呼び出すエンドポイントを、実 API の振る舞いに合わせて再現する。
// 振る舞いは dpf-go v0.6.0 の utils のテスト用サーバに倣っている。
package dpftest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Record は模擬サーバ上のレコードである。
type Record struct {
	ID          string
	Name        string
	TTL         *int32
	Rrtype      string
	Rdata       []string
	Labels      map[string]string
	Description string
	// State は 0 = 反映済み、1 = 追加予定、2 = 削除予定 である。
	State int
}

// Zone は模擬サーバ上のゾーンである。
type Zone struct {
	ID      string
	Name    string
	Records []*Record
	Labels  map[string]string
}

// AtomicRequest は一括置き換えの要求である。
type AtomicRequest struct {
	Records []struct {
		Name   string `json:"name"`
		TTL    *int32 `json:"ttl"`
		Rrtype string `json:"rrtype"`
		Rdata  []struct {
			Value string `json:"value"`
		} `json:"rdata"`
		Description string            `json:"description"`
		Labels      map[string]string `json:"labels"`
	} `json:"records"`
	Description         *string `json:"description"`
	OverwriteSoa        *bool   `json:"overwrite_soa"`
	OverwriteZoneApexNs *bool   `json:"overwrite_zone_apex_ns"`
}

// Server は模擬サーバである。フィールドの読み書きは Lock / Unlock の内側で行う。
type Server struct {
	mu sync.Mutex

	URL       string
	zones     []*Zone
	next      int
	transport http.RoundTripper

	// 呼び出しの記録。
	Atomics       int
	CurrentsGets  int
	ZoneListGets  int
	ZoneLabelPuts int
	LastAtomic    *AtomicRequest
	// Tokens はリクエストごとの Authorization ヘッダである。
	Tokens []string

	// FailLabelPutAfterAtomic が true の場合、一括置き換えの後のゾーンのラベルの更新
	// (排他の解放) を失敗させる。
	FailLabelPutAfterAtomic bool
	// FailAtomic が true の場合、一括置き換えを 500 で失敗させる。
	FailAtomic bool
	// BeforeAtomic は一括置き換えの直前に呼ばれる (ロックの内側)。
	BeforeAtomic func(s *Server)
}

// New は模擬サーバを HTTP で起動する。テストの終了時に停止する。
func New(t testing.TB, zones ...*Zone) *Server {
	t.Helper()
	return start(t, false, zones)
}

// NewTLS は模擬サーバを HTTPS で起動する。接続には Transport を用いる。
// solver 設定の endpoint は https を必須とするため、solver を通す検証に用いる。
func NewTLS(t testing.TB, zones ...*Zone) *Server {
	t.Helper()
	return start(t, true, zones)
}

// Transport は模擬サーバの証明書を信頼する RoundTripper を返す。
func (s *Server) Transport() http.RoundTripper { return s.transport }

func start(t testing.TB, tls bool, zones []*Zone) *Server {
	s := &Server{zones: zones, transport: http.DefaultTransport}
	for _, z := range zones {
		if z.Labels == nil {
			z.Labels = map[string]string{}
		}
		for _, r := range z.Records {
			s.ensureID(r)
		}
	}
	var srv *httptest.Server
	if tls {
		srv = httptest.NewTLSServer(http.HandlerFunc(s.serve))
		s.transport = srv.Client().Transport
	} else {
		srv = httptest.NewServer(http.HandlerFunc(s.serve))
	}
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// Lock は記録を読むためにサーバの状態を固定する。
func (s *Server) Lock() { s.mu.Lock() }

// Unlock は Lock の対である。
func (s *Server) Unlock() { s.mu.Unlock() }

// Zone は ID でゾーンを返す。呼び出し側は Lock を保持すること。
func (s *Server) Zone(id string) *Zone {
	for _, z := range s.zones {
		if z.ID == id {
			return z
		}
	}
	return nil
}

// Find は名前と種別で反映済みのレコードを返す。呼び出し側は Lock を保持すること。
func (z *Zone) Find(name, rrtype string) *Record {
	for _, r := range z.Records {
		if r.Name == name && r.Rrtype == rrtype && r.State == 0 {
			return r
		}
	}
	return nil
}

func (s *Server) ensureID(r *Record) {
	if r.ID == "" {
		s.next++
		r.ID = fmt.Sprintf("r%013d", s.next) // ID は 14 文字 (API の制約)
	}
	if r.Labels == nil {
		r.Labels = map[string]string{}
	}
}

const requestID = "req00000000000000000000000000001"

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	b, _ := json.Marshal(v)
	_, _ = w.Write(b)
}

func writeAccepted(w http.ResponseWriter) {
	writeJSON(w, http.StatusAccepted, map[string]any{
		"request_id": requestID, "jobs_url": "http://x/jobs/" + requestID,
	})
}

func writeError(w http.ResponseWriter, code int, errType, detailCode, attr string) {
	body := map[string]any{
		"request_id": requestID, "error_type": errType, "error_message": errType,
	}
	if detailCode != "" {
		body["error_details"] = []any{map[string]string{"code": detailCode, "attribute": attr}}
	}
	writeJSON(w, code, body)
}

func recordJSON(r *Record) map[string]any {
	rdata := make([]any, 0, len(r.Rdata))
	for _, v := range r.Rdata {
		rdata = append(rdata, map[string]string{"value": v})
	}
	labels := r.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	var ttl any
	if r.TTL != nil {
		ttl = *r.TTL
	}
	return map[string]any{
		"id": r.ID, "name": r.Name, "ttl": ttl, "rrtype": r.Rrtype, "rdata": rdata,
		"labels": labels, "state": r.State, "description": r.Description, "operator": nil,
	}
}

func cloneLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Tokens = append(s.Tokens, r.Header.Get("Authorization"))

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/") // domain-string: URL のパスの分割

	if len(parts) == 2 && parts[0] == "jobs" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{
			"request_id": parts[1], "status": "SUCCESSFUL", "resources_url": "http://x/resources",
		})
		return
	}
	if len(parts) == 1 && parts[0] == "zones" && r.Method == http.MethodGet {
		s.ZoneListGets++
		results := []any{}
		// ExecuteAll の 2 ページ目 (offset > 0) は空を返す。
		if r.URL.Query().Get("offset") == "" || r.URL.Query().Get("offset") == "0" {
			for _, z := range s.zones {
				results = append(results, map[string]any{
					"id": z.ID, "common_config_id": 1, "service_code": "dpm0000001", "state": 1,
					"favorite": 1, "name": z.Name, "network": nil, "description": "",
				})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"request_id": requestID, "results": results})
		return
	}
	if len(parts) < 2 || parts[0] != "zones" {
		http.NotFound(w, r)
		return
	}
	z := s.Zone(parts[1])
	if z == nil {
		writeError(w, http.StatusNotFound, "NotFound", "not_found", "zone")
		return
	}
	rest := parts[2:]

	switch {
	case len(rest) == 1 && rest[0] == "labels" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"request_id": requestID, "result": map[string]any{"labels": z.Labels},
		})

	case len(rest) == 1 && rest[0] == "labels" && r.Method == http.MethodPut:
		s.ZoneLabelPuts++
		if s.FailLabelPutAfterAtomic && s.Atomics > 0 {
			writeError(w, http.StatusInternalServerError, "SystemError", "", "")
			return
		}
		var body struct {
			Labels map[string]string `json:"labels"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		if body.Labels == nil {
			body.Labels = map[string]string{}
		}
		z.Labels = body.Labels
		writeAccepted(w)

	case len(rest) == 1 && rest[0] == "atomic_changes" && r.Method == http.MethodPatch:
		if s.BeforeAtomic != nil {
			s.BeforeAtomic(s)
		}
		s.Atomics++
		if s.FailAtomic {
			writeError(w, http.StatusInternalServerError, "SystemError", "", "")
			return
		}
		var req AtomicRequest
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &req); err != nil {
			writeError(w, http.StatusBadRequest, "ParameterError", "invalid", "body")
			return
		}
		s.LastAtomic = &req
		s.applyAtomic(z, &req)
		writeAccepted(w)

	case len(rest) == 2 && rest[0] == "records" && rest[1] == "currents" && r.Method == http.MethodGet:
		s.CurrentsGets++
		results := []any{}
		if r.URL.Query().Get("offset") == "" || r.URL.Query().Get("offset") == "0" {
			for _, rec := range z.Records {
				if rec.State == 1 {
					continue
				}
				results = append(results, recordJSON(rec))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"request_id": requestID, "results": results})

	case len(rest) == 1 && rest[0] == "records" && r.Method == http.MethodGet:
		results := []any{}
		if r.URL.Query().Get("offset") == "" || r.URL.Query().Get("offset") == "0" {
			for _, rec := range z.Records {
				results = append(results, recordJSON(rec))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"request_id": requestID, "results": results})

	case len(rest) == 1 && rest[0] == "records" && r.Method == http.MethodPost:
		var body struct {
			Name   string `json:"name"`
			Rrtype string `json:"rrtype"`
			Rdata  []struct {
				Value string `json:"value"`
			} `json:"rdata"`
			Labels map[string]string `json:"labels"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		for _, rec := range z.Records {
			if rec.Name == body.Name && rec.Rrtype == body.Rrtype && rec.State != 2 {
				writeError(w, http.StatusBadRequest, "ParameterError", "duplicated", "record")
				return
			}
		}
		rec := &Record{Name: body.Name, Rrtype: body.Rrtype, Labels: body.Labels, State: 1}
		for _, v := range body.Rdata {
			rec.Rdata = append(rec.Rdata, v.Value)
		}
		s.ensureID(rec)
		z.Records = append(z.Records, rec)
		writeAccepted(w)

	case len(rest) == 3 && rest[0] == "records" && rest[2] == "changes" && r.Method == http.MethodDelete:
		kept := z.Records[:0:0]
		found := false
		for _, rec := range z.Records {
			if rec.ID == rest[1] {
				found = true
				if rec.State == 1 {
					continue
				}
				rec.State = 0
			}
			kept = append(kept, rec)
		}
		if !found {
			writeError(w, http.StatusNotFound, "NotFound", "not_found", "record")
			return
		}
		z.Records = kept
		writeAccepted(w)

	case len(rest) == 2 && rest[0] == "records" && r.Method == http.MethodDelete:
		for _, rec := range z.Records {
			if rec.ID == rest[1] {
				rec.State = 2
				writeAccepted(w)
				return
			}
		}
		writeError(w, http.StatusNotFound, "NotFound", "not_found", "record")

	default:
		http.NotFound(w, r)
	}
}

// applyAtomic は一括置き換えを適用する。未反映の編集を破棄し、要求のレコードで
// 置き換える。取り込まないフラグが立っている SOA と Zone Apex の NS は既存のものを維持する。
func (s *Server) applyAtomic(z *Zone, req *AtomicRequest) {
	overwriteSoa := req.OverwriteSoa == nil || *req.OverwriteSoa
	overwriteNS := req.OverwriteZoneApexNs == nil || *req.OverwriteZoneApexNs
	isApexNS := func(name, rrtype string) bool { return rrtype == "NS" && name == z.Name }

	var kept []*Record
	for _, r := range z.Records {
		if r.State == 1 {
			continue
		}
		r.State = 0
		kept = append(kept, r)
	}
	var next []*Record
	for _, rr := range req.Records {
		if (!overwriteSoa && rr.Rrtype == "SOA") || (!overwriteNS && isApexNS(rr.Name, rr.Rrtype)) {
			continue
		}
		rec := &Record{Name: rr.Name, TTL: rr.TTL, Rrtype: rr.Rrtype, Description: rr.Description,
			Labels: cloneLabels(rr.Labels)}
		for _, v := range rr.Rdata {
			rec.Rdata = append(rec.Rdata, v.Value)
		}
		for _, old := range kept {
			if old.Name == rr.Name && old.Rrtype == rr.Rrtype {
				rec.ID = old.ID
			}
		}
		s.ensureID(rec)
		next = append(next, rec)
	}
	for _, old := range kept {
		if (!overwriteSoa && old.Rrtype == "SOA") || (!overwriteNS && isApexNS(old.Name, old.Rrtype)) {
			next = append(next, old)
		}
	}
	z.Records = next
}

// StandardZone は SOA と Zone Apex の NS を持つゾーンを返す。
func StandardZone(id, name string) *Zone {
	ttl := int32(3600)
	return &Zone{
		ID:   id,
		Name: name,
		Records: []*Record{
			{Name: name, TTL: &ttl, Rrtype: "SOA", Rdata: []string{"ns.example. root.example. 1 3600 900 604800 300"}},
			{Name: name, TTL: &ttl, Rrtype: "NS", Rdata: []string{"ns1.example.", "ns2.example."}},
		},
	}
}
