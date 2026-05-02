// Package mempalace implements the lifetime semantic memory store
// (sqlite + sqlite-vec) exposed as MCP tools recall/remember/forget.
// Ships in v0.2.
package mempalace

func Help() string {
	return `mempalace — lifetime semantic memory store backed by sqlite + sqlite-vec.

Ships in v0.2.

Future commands:
  yashigatakae mempalace recall <query>     # search memory by similarity
  yashigatakae mempalace remember <text>    # add a memory entry
  yashigatakae mempalace forget <id>        # remove an entry
  yashigatakae mempalace stats              # entry count, embedding stats, last sweep`
}
