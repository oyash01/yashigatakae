// Package kintsugi gives cross-device session + working-tree continuity.
// "Resume the same Claude Code conversation at the same checkpoint
// on any machine, with the same uncommitted changes intact."
// Ships in v0.3.
package kintsugi

func HelpHandoff() string {
	return `handoff — explicit checkpoint before switching machines.

Ships in v0.3.

  yashigatakae handoff [--note "..."]
  -> flushes the watcher, marks the session+worktree as transferable,
     prints a short resume code (e.g. ABC-123)`
}

func HelpResume() string {
	return `resume — restore a synced session+worktree on this machine.

Ships in v0.3.

  yashigatakae resume                    # latest active session from any machine
  yashigatakae resume <resume-code>      # specific handoff code

If local working tree has divergent uncommitted changes, prompts:
  keep-local / keep-remote / save-local-as-stash-then-take-remote.`
}

func HelpSessions() string {
	return `sessions — list/diff/manage synced sessions.

Ships in v0.3.

  yashigatakae sessions ls
  yashigatakae sessions diff <id>          # show what would change locally
  yashigatakae sessions abandon <id>       # mark dead, stop syncing
  yashigatakae sessions checkpoints <id>   # list time-ordered checkpoints
  yashigatakae sessions rollback <id> <ts> # restore a past checkpoint`
}
