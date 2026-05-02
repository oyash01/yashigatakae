// Package graphify generates per-repo codebase wikis (overview, modules,
// graph.json, api, conventions, glossary, recent, index) so Claude Code
// reads a map instead of grepping the repo on every session.
// Ships in v0.4.
package graphify

func Help() string {
	return `graphify — codebase wiki generator. LSP + tree-sitter + Claude prose.

Ships in v0.4.

Future commands:
  yashigatakae graphify <repo>          # generate wiki for repo (cached)
  yashigatakae graphify <repo> refresh  # force regenerate
  yashigatakae graphify ls              # list indexed repos`
}
