package mcptools

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"strings"
)

// BuildRequestParts substitutes path params and encodes query params from
// flat tool arguments into an API request path and raw query string.
func BuildRequestParts(tool *ToolDef, args map[string]any) (string, string, error) {
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
		s := ArgToString(v)
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

// ArgToString renders one tool argument as a query/path string.
func ArgToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%v", val)
	case bool:
		return fmt.Sprintf("%t", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// BodyArg returns the request body argument, or nil when absent.
func BodyArg(tool *ToolDef, args map[string]any) any {
	if tool.BodySchema == nil {
		return nil
	}
	if raw, ok := args["body"]; ok {
		return raw
	}
	return nil
}

// IdempotencyKey derives a deterministic key per (token, operation, args).
// An identical retry — transport hiccup, agent re-roll — replays the stored
// 2xx response instead of double-posting financial data; a deliberate second
// identical create inside the 24h window also replays, which for an ERP is
// duplicate suppression, and the replayed record is visible in the result.
func IdempotencyKey(tokenID int, op string, args map[string]any) string {
	clean := maps.Clone(args)
	delete(clean, "_idempotency_key")
	var b strings.Builder
	fmt.Fprintf(&b, "%d\x00%s", tokenID, op)
	// json.Marshal sorts map keys, so the encoding is canonical.
	encoded, _ := json.Marshal(clean)
	b.Write(encoded)
	sum := sha256.Sum256([]byte(b.String()))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

// TextResult wraps a JSON response body as one text content block,
// truncated at 64 KiB.
func TextResult(data []byte) map[string]any {
	const maxText = 64 << 10
	text := string(data)
	if len(text) > maxText {
		text = text[:maxText] + ` …{"truncated":true}`
	}
	return map[string]any{"content": []any{
		map[string]any{"type": "text", "text": text},
	}}
}

// PDFResult wraps a PDF body as a base64 resource block, capped at 2 MiB.
func PDFResult(path string, data []byte) map[string]any {
	const maxBlob = 2 << 20
	if len(data) > maxBlob {
		return ErrorResult("PDF exceeds the 2 MiB tool limit — fetch it from the web UI")
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

// ErrorResult is a tool-result failure (isError), never a protocol error.
func ErrorResult(msg string) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": msg}},
		"isError": true,
	}
}
