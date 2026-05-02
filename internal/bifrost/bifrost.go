// Package bifrost implements the single MCP gateway that fans out to N MCP servers.
// Ships in v0.2.
package bifrost

func Help() string {
	return `bifrost — single MCP endpoint that fans out to N downstream MCP servers.

Ships in v0.2 along with mempalace.

Future commands:
  yashigatakae bifrost up      # start gateway daemon (VPS-side)
  yashigatakae bifrost down    # stop daemon
  yashigatakae bifrost reload  # reload routing config
  yashigatakae bifrost tools   # list registered tools across all downstreams`
}
