package main

import (
	"fmt"

	"github.com/naufal/latasya-erp/internal/mcptools"
)

// dispatcher executes one tools/call against the ERP API. It owns every ERP
// wire convention so the JSON-RPC layer knows nothing about them; the shared
// request-building and result-shaping live in internal/mcptools.
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
		return mcptools.ErrorResult(fmt.Sprintf("unknown tool %q — see tools/list", name))
	}

	path, query, err := mcptools.BuildRequestParts(tool, args)
	if err != nil {
		return mcptools.ErrorResult(err.Error())
	}

	var key string
	if tool.Mutation {
		key = mcptools.IdempotencyKey(d.tokenID, name, args)
		if explicit, ok := args["_idempotency_key"].(string); ok && explicit != "" {
			key = explicit
		}
	}

	data, _, err := d.client.call(tool.Method, path+query, mcptools.BodyArg(tool, args), key, "")
	if err != nil {
		return mcptools.ErrorResult(redact(err.Error(), d.client.tok))
	}

	if tool.IsPDF {
		return mcptools.PDFResult(path, data)
	}
	return mcptools.TextResult(data)
}
