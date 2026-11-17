// Package mcp is the P7.3 MCP trust slice: a governed bridge between the
// VibeSDK agent runtime and external MCP servers.
//
// Trust rule (normative): MCP servers are untrusted external boundaries.
// Discovery produces read-only metadata; it never grants execution. Every
// invocation requires an explicit, tenant-scoped grant that records who
// approved it, and every call — granted or denied — leaves an audit event.
//
// What this package is NOT (v1): a transport for the MCP wire protocol
// (stdio/SSE), a credential vault, or an MCP catalog/marketplace. Those are
// M1/M5 work. It defines the boundary contracts (registry, policy, grant,
// execution gate, audit sink) so the first real transport can land behind
// them without widening the trust surface.
package mcp
