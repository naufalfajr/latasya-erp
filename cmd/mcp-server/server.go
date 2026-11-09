package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/naufal/latasya-erp/internal/mcptools"
)

const protocolVersion = "2025-06-18"

const serverInstructions = `Tools map 1:1 to Latasya ERP API operations and are scoped to your token's capabilities (server-enforced). Amounts are integer IDR strings ("50000"); quantities decimal strings like "1.50"; dates YYYY-MM-DD. Resolve ids first (list tools) when a name is given. For mutations you may retry, pass the same _idempotency_key to replay instead of duplicating.`

// jsonrpc frames the MCP stdio protocol: newline-delimited JSON-RPC 2.0.
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
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// mcpServer owns the stdio loop. The protocol layer knows nothing about the
// ERP; the dispatcher knows nothing about JSON-RPC.
type mcpServer struct {
	disp *dispatcher
}

func (s *mcpServer) serve(r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	out := bufio.NewWriter(w)
	defer out.Flush()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req jsonrpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.write(out, jsonrpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &jsonrpcError{Code: codeParseError, Message: "parse error"}})
			continue
		}
		if strings.HasPrefix(req.Method, "notifications/") {
			continue // notifications get no response
		}
		if len(req.ID) == 0 {
			continue // nameless non-notification: ignore per spec
		}
		resp := jsonrpcResponse{JSONRPC: "2.0", ID: req.ID}
		result, rerr := s.handle(req)
		if rerr != nil {
			resp.Error = rerr
		} else {
			resp.Result = result
		}
		s.write(out, resp)
	}
	if err := scanner.Err(); err != nil {
		// A line beyond the scanner cap (or a broken pipe): answer with one
		// parse error so the client learns why the server stopped instead
		// of seeing a bare EOF.
		s.write(out, jsonrpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &jsonrpcError{Code: codeParseError, Message: "request line exceeds 4 MiB limit"}})
		log.Printf("stdin closed: %v", err)
	}
}

func (s *mcpServer) write(out *bufio.Writer, resp jsonrpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	out.Write(data)
	out.WriteByte('\n')
	out.Flush()
}

func (s *mcpServer) handle(req jsonrpcRequest) (any, *jsonrpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "latasya-erp", "version": version},
			"instructions":    serverInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return s.toolsList(), nil
	case "tools/call":
		return s.toolsCall(req.Params)
	default:
		return nil, &jsonrpcError{Code: codeMethodNotFound, Message: "method not supported: " + req.Method}
	}
}

// toolsList filters by the token's scopes (UX only; the server re-enforces
// every call) and sorts by name for stable diffs.
func (s *mcpServer) toolsList() map[string]any {
	scopeSet := make(map[string]bool, len(s.disp.scopes))
	for _, sc := range s.disp.scopes {
		scopeSet[sc] = true
	}
	entries := make([]mcptools.ListEntry, 0, len(mcptools.GeneratedTools))
	for _, t := range mcptools.GeneratedTools {
		if t.Capability != "" && !scopeSet[t.Capability] {
			continue
		}
		entries = append(entries, t.ToListEntry())
	}
	return map[string]any{"tools": entries}
}

func (s *mcpServer) toolsCall(params json.RawMessage) (any, *jsonrpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpcError{Code: codeInvalidParams, Message: "invalid tools/call params"}
	}
	args := map[string]any{}
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return nil, &jsonrpcError{Code: codeInvalidParams, Message: "arguments must be a JSON object"}
		}
	}
	return s.disp.callTool(p.Name, args), nil
}

// runServe is the default subcommand: serve MCP over stdio using the stored
// credential.
func runServe() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := requireHTTPS(cfg.BaseURL); err != nil {
		return err
	}
	disp := &dispatcher{client: newAPIClient(cfg.BaseURL, cfg.Token), tokenID: cfg.TokenID, scopes: cfg.Scopes}
	server := &mcpServer{disp: disp}
	// Logs must go to stderr: stdout is the protocol channel.
	log.SetOutput(os.Stderr)
	server.serve(os.Stdin, os.Stdout)
	return nil
}

// requireHTTPS refuses to send credentials over plain HTTP. Loopback is
// exempt; LATASYA_MCP_ALLOW_HTTP=true is the local-development escape hatch.
// Every flow that transmits the device code or token — login, serve, logout —
// must call it.
func requireHTTPS(baseURL string) error {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid base URL %q", baseURL)
	}
	host := u.Hostname()
	loopback := host == "127.0.0.1" || host == "localhost" || host == "::1"
	if u.Scheme != "https" && !loopback && os.Getenv("LATASYA_MCP_ALLOW_HTTP") != "true" {
		return fmt.Errorf("refusing to send credentials over plain HTTP to %s (set LATASYA_MCP_ALLOW_HTTP=true to override for local development)", baseURL)
	}
	return nil
}
