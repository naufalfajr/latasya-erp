package v1_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/naufal/latasya-erp/internal/model"
)

func TestOpenAPIContract_SpecValid(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("load openapi spec: %v", err)
	}

	ctx := context.Background()
	if err := doc.Validate(ctx); err != nil {
		t.Fatalf("openapi spec validation failed: %v", err)
	}

	t.Logf("OpenAPI spec valid: %s v%s", doc.Info.Title, doc.Info.Version)
	t.Logf("Paths defined: %d", len(doc.Paths.Map()))
}

// TestOpenAPIContract_CapabilityAnnotations locks the x-latasya-* extensions
// that drive MCP tool generation and documentation: operationIds unique,
// capability values real, and every mutating operation classified (a
// capability or an explicit auth class) so no endpoint silently ships
// un-gated.
func TestOpenAPIContract_CapabilityAnnotations(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("load openapi spec: %v", err)
	}

	validCaps := make(map[string]bool, len(model.AllCapabilities))
	for _, c := range model.AllCapabilities {
		validCaps[c] = true
	}
	validAuth := map[string]bool{"public": true, "any": true, "cookie-only": true, "bearer-only": true}

	seen := map[string]string{}
	ops := 0
	for path, item := range doc.Paths.Map() {
		methods := map[string]*openapi3.Operation{
			"get": item.Get, "post": item.Post, "put": item.Put,
			"patch": item.Patch, "delete": item.Delete,
		}
		for method, op := range methods {
			if op == nil {
				continue
			}
			ops++
			if prev, dup := seen[op.OperationID]; dup {
				t.Errorf("operationId %s used by both %s and %s %s", op.OperationID, prev, method, path)
			} else {
				seen[op.OperationID] = method + " " + path
			}

			capVal := extString(op.Extensions, "x-latasya-capability")
			if capVal != "" && !validCaps[capVal] {
				t.Errorf("%s: x-latasya-capability %q is not a known capability", op.OperationID, capVal)
			}
			authVal := extString(op.Extensions, "x-latasya-auth")
			if authVal != "" && !validAuth[authVal] {
				t.Errorf("%s: x-latasya-auth %q is not one of public/any/cookie-only/bearer-only", op.OperationID, authVal)
			}
			if method != "get" && capVal == "" && !validAuth[authVal] {
				t.Errorf("%s (%s %s): mutating operation declares neither x-latasya-capability nor a valid x-latasya-auth class",
					op.OperationID, method, path)
			}
		}
	}
	if ops < 80 {
		t.Errorf("suspiciously few operations found: %d", ops)
	}
}

func extString(ext map[string]any, key string) string {
	v, ok := ext[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case json.RawMessage:
		var s string
		if json.Unmarshal(val, &s) == nil {
			return s
		}
	}
	return ""
}
