package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/naufal/latasya-erp/internal/mcptools"
)

// dispatcher executes one tools/call against the ERP API. It owns every ERP
// wire convention so the JSON-RPC layer knows nothing about them.
type dispatcher struct {
	client  *apiClient
	tokenID int
	scopes  []string
}

// callTool resolves the tool, builds the HTTP request from flat arguments,
// and shapes the MCP result content. Domain errors are results with
// isError:true, never protocol errors.
func (d *dispatcher) callTool(name string, args map[string]any) map[string]any {
	tool := mcptools.Find(name)
	if tool == nil {
		return errorResult(fmt.Sprintf("unknown tool %q — see tools/list", name))
	}

	path, query, err := buildRequestParts(tool, args)
	if err != nil {
		return errorResult(err.Error())
	}

	var body any
	if raw, ok := args["body"]; ok && tool.BodySchema != nil {
		body = raw
	}

	var key string
	if tool.Mutation {
		key = idempotencyKey(d.tokenID, name, args)
		if explicit, ok := args["_idempotency_key"].(string); ok && explicit != "" {
			key = explicit
		}
	}

	data, _, err := d.client.call(tool.Method, path+query, body, key, "")
	if err != nil {
		return errorResult(redact(err.Error(), d.client.tok))
	}

	if tool.IsPDF {
		return pdfResult(path, data)
	}
	return textResult(data)
}

// buildRequestParts substitutes path params and encodes query params.
func buildRequestParts(tool *mcptools.ToolDef, args map[string]any) (string, string, error) {
	path := tool.PathTmpl
	q := url.Values{}
	for _, p := range tool.Params {
		v, present := args[p.Name]
		if !present {
			if p.Required {
				return "", "", fmt.Errorf("missing required parameter %q", p.Name)
			}
			continue
		}
		var s string
		switch val := v.(type) {
		case string:
			s = val
		case float64:
			s = trimFloat(val)
		case bool:
			s = fmt.Sprintf("%t", val)
		default:
			s = fmt.Sprintf("%v", val)
		}
		if p.In == "path" {
			path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(s))
		} else {
			q.Set(p.Name, s)
		}
	}
	encoded := ""
	if len(q) > 0 {
		encoded = "?" + q.Encode()
	}
	return path, encoded, nil
}

func trimFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%v", f)
}

// idempotencyKey derives a deterministic key per (token, operation, args).
// An identical retry — transport hiccup, agent re-roll — replays the stored
// 2xx response instead of double-posting financial data; a deliberate second
// identical create inside the 24h window also replays, which for an ERP is
// duplicate suppression, and the replayed record is visible in the result.
func idempotencyKey(tokenID int, op string, args map[string]any) string {
	clean := map[string]any{}
	for k, v := range args {
		if k != "_idempotency_key" {
			clean[k] = v
		}
	}
	keys := make([]string, 0, len(clean))
	for k := range clean {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "%d\x00%s", tokenID, op)
	encoded, _ := json.Marshal(clean)
	b.Write(encoded)
	sum := sha256.Sum256([]byte(b.String()))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

// --- MCP result shapes -----------------------------------------------------

func textResult(data []byte) map[string]any {
	const maxText = 64 << 10
	text := string(data)
	if len(text) > maxText {
		text = text[:maxText] + ` …{"truncated":true}`
	}
	return map[string]any{"content": []any{
		map[string]any{"type": "text", "text": text},
	}}
}

func pdfResult(path string, data []byte) map[string]any {
	const maxBlob = 2 << 20
	if len(data) > maxBlob {
		return errorResult("PDF exceeds the 2 MiB tool limit — fetch it from the web UI")
	}
	name := strings.TrimPrefix(path, "/api/v1/")
	return map[string]any{"content": []any{
		map[string]any{"type": "text", "text": fmt.Sprintf("%s.pdf (%d bytes) — attached as a resource.", name, len(data))},
		map[string]any{"type": "resource", "resource": map[string]any{
			"uri":      "latasya://" + name,
			"mimeType": "application/pdf",
			"blob":     base64.StdEncoding.EncodeToString(data),
		}},
	}}
}

func errorResult(msg string) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": msg}},
		"isError": true,
	}
}
