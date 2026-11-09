package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// apiClient is a thin JSON-API client, ported from the Telegram bot. It adds
// no business logic: every rule lives on the server.
type apiClient struct {
	base string
	tok  string
	hc   *http.Client
}

func newAPIClient(base, token string) *apiClient {
	base = strings.TrimSuffix(base, "/")
	return &apiClient{
		base: base,
		tok:  token,
		hc:   &http.Client{Timeout: 15 * time.Second},
	}
}

// apiErr carries the server's error envelope so tool results can surface the
// code and field errors verbatim.
type apiErr struct {
	Status  int
	Message string            `json:"error"`
	Code    string            `json:"code"`
	Fields  map[string]string `json:"fields"`
}

func (e *apiErr) Error() string {
	if len(e.Fields) > 0 {
		return fmt.Sprintf("erp api %d %s: %s (fields: %v)", e.Status, e.Code, e.Message, e.Fields)
	}
	return fmt.Sprintf("erp api %d %s: %s", e.Status, e.Code, e.Message)
}

// call performs one request. idempotencyKey is set on mutations. A nil body
// means no request body; out receives the raw response bytes.
func (a *apiClient) call(method, path string, body any, idempotencyKey string, wantContentType string) ([]byte, string, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, a.base+path, reader)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 300 {
		e := &apiErr{Status: resp.StatusCode}
		_ = json.Unmarshal(data, e)
		if e.Message == "" {
			e.Message = strings.TrimSpace(string(data))
		}
		return nil, "", e
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// redact hides the bearer token if it ever leaks into an error string.
func redact(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "lat_****")
}
