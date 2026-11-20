package openapi

import _ "embed"

// Capabilities is the API's capability table (GET /api/v1/public/capabilities,
// api-gap-audit G2): every public capability once, with its name on the API,
// MCP and CLI. Refresh with `make capabilities`; cmd's TestCapabilityTable
// fails when a row names a command the CLI doesn't have, or a command has no row.
//
//go:embed capabilities.json
var Capabilities []byte
