package v1_test

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
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
