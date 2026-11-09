// Package mcpserver serves the ERP's remote MCP endpoint: a stateless
// Streamable-HTTP JSON-RPC surface at POST /mcp that exposes every tool from
// the generated mcptools table. Tool calls are replayed through the existing
// /api/v1 handler tree, so authentication, capability gates, idempotency,
// and audit are the server's own — this package owns only the protocol.
package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/mcptools"
)

const (
	ProtocolVersion    = "2025-06-18"
	serverName         = "latasya-erp"
	maxBody            = 1 << 20
	serverInstructions = `Tools map 1:1 to Latasya ERP API operations and are scoped to your token's capabilities (server-enforced). Amounts are integer IDR strings ("50000"); quantities decimal strings like "1.50"; dates YYYY-MM-DD. Resolve ids first (list tools) when a name is given. For mutations you may retry, pass the same _idempotency_key to replay instead of duplicating.`
)

// Server handles POST /mcp. API is the /api/v1 handler tree (the apiMux);
// replayed requests carry the caller's Authorization header so they
// re-authenticate through the standard middleware.
var Version = "dev"

type Server struct {
	API http.Handler
}
type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

const (
	codeParseError     = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Browsers always attach Origin to cross-origin POSTs and cannot send a
	// JSON Content-Type without a CORS preflight (which this server never
	// approves). Checking Origin closes DNS-rebinding and cross-site form
	// tricks on the cookie path; bearer callers are unaffected.
	if origin := r.Header.Get("Origin"); origin != "" && !sameHost(origin, r.Host) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeJSONRPC(w, http.StatusBadRequest, jsonrpcResponse{JSONRPC: "2.0",
			ID: json.RawMessage("null"), Error: &jsonrpcError{codeParseError, "body exceeds 1 MiB limit"}})
		return
	}
	var req jsonrpcRequest
	if err := json.Unmarshal(body, &req); err != nil || req.JSONRPC == "" {
		writeJSONRPC(w, http.StatusBadRequest, jsonrpcResponse{JSONRPC: "2.0",
			ID: json.RawMessage("null"), Error: &jsonrpcError{codeParseError, "parse error"}})
		return
	}

	// Notifications (no id) get no response; JSON-RPC batching was removed
	// in MCP 2025-06-18 and an array fails the unmarshal above as -32700.
	if strings.HasPrefix(req.Method, "notifications/") || len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	resp := jsonrpcResponse{JSONRPC: "2.0", ID: req.ID}
	result, rerr := s.handle(r, req)
	if rerr != nil {
		resp.Error = rerr
	} else {
		resp.Result = result
	}
	writeJSONRPC(w, http.StatusOK, resp)
}

func (s *Server) handle(r *http.Request, req jsonrpcRequest) (any, *jsonrpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": negotiated(req.Params),
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": serverName, "version": Version},
			"instructions":    serverInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return s.toolsList(r), nil
	case "tools/call":
		return s.toolsCall(r, req.Params)
	default:
		return nil, &jsonrpcError{codeMethodNotFound, "method not supported: " + req.Method}
	}
}

func negotiated(params json.RawMessage) string {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)
	if p.ProtocolVersion == "2025-06-18" || p.ProtocolVersion == "2025-03-26" {
		return p.ProtocolVersion
	}
	return ProtocolVersion
}

func (s *Server) toolsList(r *http.Request) map[string]any {
	caps := v1.EffectiveCapabilitiesFromContext(r.Context())
	held := make(map[string]bool, len(caps))
	for _, c := range caps {
		held[c] = true
	}
	entries := make([]mcptools.ListEntry, 0, len(mcptools.GeneratedTools))
	for i := range mcptools.GeneratedTools {
		t := &mcptools.GeneratedTools[i]
		if t.Capability != "" && !held[t.Capability] {
			continue
		}
		entries = append(entries, t.ToListEntry())
	}
	return map[string]any{"tools": entries}
}

func (s *Server) toolsCall(r *http.Request, params json.RawMessage) (any, *jsonrpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpcError{codeInvalidParams, "invalid tools/call params"}
	}
	args := map[string]any{}
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return nil, &jsonrpcError{codeInvalidParams, "arguments must be a JSON object"}
		}
	}

	tool := mcptools.Find(p.Name)
	// A capability the caller lacks is indistinguishable from an unknown
	// tool: no probing oracle.
	caps := v1.EffectiveCapabilitiesFromContext(r.Context())
	if tool == nil || (tool.Capability != "" && !slices.Contains(caps, tool.Capability)) {
		return nil, &jsonrpcError{codeInvalidParams, fmt.Sprintf("unknown tool %q — see tools/list", p.Name)}
	}

	path, query, err := mcptools.BuildRequestParts(tool, args)
	if err != nil {
		return mcptools.ErrorResult(err.Error()), nil
	}

	var body io.Reader
	if b := mcptools.BodyArg(tool, args); b != nil {
		encoded, err := json.Marshal(b)
		if err != nil {
			return mcptools.ErrorResult(err.Error()), nil
		}
		body = bytes.NewReader(encoded)
	}

	replay, err := http.NewRequest(tool.Method, "http://mcp.internal"+path+query, body)
	if err != nil {
		return mcptools.ErrorResult(err.Error()), nil
	}
	replay = replay.WithContext(r.Context())
	replay.Header.Set("Authorization", r.Header.Get("Authorization"))
	if body != nil {
		replay.Header.Set("Content-Type", "application/json")
	}
	if tool.Mutation {
		key := mcptools.IdempotencyKey(tokenIDFrom(r), p.Name, args)
		if explicit, ok := args["_idempotency_key"].(string); ok && explicit != "" {
			key = explicit
		}
		replay.Header.Set("Idempotency-Key", key)
	}

	rec := httptest.NewRecorder()
	s.API.ServeHTTP(rec, replay)

	if rec.Code >= 200 && rec.Code < 300 {
		if tool.IsPDF {
			return mcptools.PDFResult(path, rec.Body.Bytes()), nil
		}
		return mcptools.TextResult(rec.Body.Bytes()), nil
	}
	return mcptools.ErrorResult(fmt.Sprintf("erp api %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))), nil
}

func tokenIDFrom(r *http.Request) int {
	if id := v1.TokenIDFromContext(r.Context()); id != nil {
		return *id
	}
	return 0
}

func writeJSONRPC(w http.ResponseWriter, status int, resp jsonrpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

// sameHost reports whether an Origin header value targets this server
// (scheme-insensitive; host[:port] must match).
func sameHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Host == host
}
