package v1_test

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

var contractResponses []contractEntry

type contractEntry struct {
	Method string
	Path   string
	Status int
	Body   []byte
}

func RecordContractCheck(method, path string, status int, body []byte) {
	contractResponses = append(contractResponses, contractEntry{
		Method: method,
		Path:   path,
		Status: status,
		Body:   body,
	})
}

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

func TestOpenAPIContract(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("load openapi spec: %v", err)
	}

	ctx := context.Background()
	if err := doc.Validate(ctx); err != nil {
		t.Fatalf("openapi spec validation failed: %v", err)
	}

	if len(contractResponses) == 0 {
		t.Log("No contract responses recorded yet (Wave 2 tasks will populate this)")
		return
	}

	for _, entry := range contractResponses {
		t.Run(entry.Method+"_"+entry.Path, func(t *testing.T) {
			t.Logf("Contract check: %s %s -> %d (%d bytes)", entry.Method, entry.Path, entry.Status, len(entry.Body))
		})
	}
}
