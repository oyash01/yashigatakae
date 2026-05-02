// Package hermes implements the self-learning background agent that runs
// queued Claude tasks on the VPS 24/7 and distills lessons into mempalace.
// Ships in v0.5.
package hermes

func Help() string {
	return `hermes — self-learning background agent on the VPS.

Ships in v0.5.

Future commands:
  yashigatakae hermes enqueue --project X --prompt "..."  # queue a task
  yashigatakae hermes ls                                  # list queued/running/done
  yashigatakae hermes logs <id>                           # tail logs
  yashigatakae hermes cancel <id>                         # cancel a task

Tasks run claude in non-interactive mode; after each run, the agent asks
Claude for 1-5 lessons learned and writes them to mempalace tagged "lesson".`
}
