package mcptools_test

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/naufal/latasya-erp/internal/mcptools"
)

// TestGeneratedToolsCoverTheSpec is the coverage contract: every spec
// operation is either a generated tool or explicitly excluded, and nothing
// exists on both sides. A new endpoint fails here until classified.
func TestGeneratedToolsCoverTheSpec(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}

	specOps := map[string]bool{}
	for _, item := range doc.Paths.Map() {
		for _, op := range []*openapi3.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op != nil {
				specOps[op.OperationID] = true
			}
		}
	}

	generated := map[string]bool{}
	for _, tool := range mcptools.GeneratedTools {
		if !specOps[tool.Name] {
			t.Errorf("generated tool %q does not exist in the spec", tool.Name)
		}
		generated[tool.Name] = true
	}
	for name := range mcptools.ExcludedOperations {
		if !specOps[name] {
			t.Errorf("excluded operation %q does not exist in the spec", name)
		}
		if generated[name] {
			t.Errorf("operation %q is both generated and excluded", name)
		}
	}
	for name := range specOps {
		if !generated[name] {
			if _, excluded := mcptools.ExcludedOperations[name]; !excluded {
				t.Errorf("operation %q is neither generated nor excluded — classify it", name)
			}
		}
	}
	if len(generated) < 70 {
		t.Errorf("suspiciously few generated tools: %d", len(generated))
	}
}

// TestInputSchemasAreSelfContained guards the $ref resolution: the generated
// body schemas must not contain unresolved component pointers, which an MCP
// client cannot follow.
func TestInputSchemasAreSelfContained(t *testing.T) {
	for _, tool := range mcptools.GeneratedTools {
		if len(tool.BodySchema) == 0 {
			continue
		}
		if schema := string(tool.InputSchema()); strings.Contains(schema, "$ref") ||
			strings.Contains(schema, "#/components/") {
			t.Errorf("%s: input schema contains unresolved $ref pointers", tool.Name)
		}
	}
}
