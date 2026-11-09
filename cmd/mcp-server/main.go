// latasya-mcp is the MCP server binary for Latasya ERP: it runs next to an
// AI client (Claude Desktop, Claude Code, Codex, …), speaks MCP 2025-06-18
// over stdio, and calls the ERP's existing JSON API with the user's own
// Bearer token. It imports no ERP internals and holds no business rules.
//
// Usage:
//
//	latasya-mcp login --base-url https://erp.example.com   # once per ~90 days
//	latasya-mcp status
//	latasya-mcp logout
//	latasya-mcp serve                                       # default; stdio MCP
//
// Environment: LATASYA_API_URL (default http://127.0.0.1:8080),
// LATASYA_MCP_CONFIG (config path), LATASYA_MCP_TOKEN (headless override),
// LATASYA_MCP_ALLOW_HTTP=true (dev only).
package main

import (
	"flag"
	"fmt"
	"os"
)

// version identifies the build; overridden via -ldflags.
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "login":
			fs := flag.NewFlagSet("login", flag.ExitOnError)
			base := fs.String("base-url", envBaseURL(), "ERP base URL, e.g. https://erp.example.com")
			fs.Parse(os.Args[2:])
			if err := runLogin(*base); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "status":
			if err := runStatus(); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "logout":
			if err := runLogout(); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "serve", "--serve":
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		default:
			// Unknown subcommand: fall through to serve so MCP clients that
			// launch the binary with extra args still get a server.
		}
	}
	if err := runServe(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
