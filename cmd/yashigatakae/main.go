// Command yashigatakae is the Claude Code orchestrator + lifetime memory + cross-device continuity tool.
// See https://github.com/oyash01/yashigatakae for the full design.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/oyash01/yashigatakae/internal/atrest"
	"github.com/oyash01/yashigatakae/internal/bifrost"
	"github.com/oyash01/yashigatakae/internal/caveman"
	"github.com/oyash01/yashigatakae/internal/doctor"
	"github.com/oyash01/yashigatakae/internal/graphify"
	"github.com/oyash01/yashigatakae/internal/hermes"
	yashihooks "github.com/oyash01/yashigatakae/internal/hooks"
	"github.com/oyash01/yashigatakae/internal/kintsugi"
	"github.com/oyash01/yashigatakae/internal/mempalace"
	"github.com/oyash01/yashigatakae/internal/secrets"
	"github.com/oyash01/yashigatakae/internal/osdetect"
	"github.com/oyash01/yashigatakae/internal/state"
	"github.com/oyash01/yashigatakae/internal/tui"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X main.Version=v0.1.0".
var Version = "v0.1.0-dev"

func main() {
	root := &cobra.Command{
		Use:           "yashigatakae",
		Short:         "Claude Code orchestrator + lifetime memory + cross-device continuity",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: false,
		// No-args opens the Bubble Tea root menu. Subcommands bypass this.
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) > 0 {
				return c.Help()
			}
			return runInteractive()
		},
	}

	root.AddCommand(newInitCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newCavemanCmd())
	root.AddCommand(newStateCmd())
	root.AddCommand(newSecretsCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newUpgradeCmd())
	root.AddCommand(newMempalaceCmd())
	root.AddCommand(newBifrostCmd())
	root.AddCommand(newHooksCmd())
	root.AddCommand(newKintsugiCmd())
	root.AddCommand(newGraphifyCmd())
	root.AddCommand(newHermesCmd())
	root.AddCommand(newHandoffCmd())
	root.AddCommand(newResumeCmd())
	root.AddCommand(newSessionsCmd())
	root.AddCommand(newBackfillCmd())
	root.AddCommand(notYet("link", "v0.6", state.HelpLink))
	root.AddCommand(&cobra.Command{
		Use:   "enable",
		Short: "Wire all yashigatakae services to start at boot and restart forever (Linux/systemd; no-op on Mac/Win)",
		RunE: func(c *cobra.Command, args []string) error {
			return state.Enable()
		},
	})
	root.AddCommand(&cobra.Command{
		Use:   "disable",
		Short: "Stop and disable all yashigatakae services (Linux/systemd; no-op on Mac/Win)",
		RunE: func(c *cobra.Command, args []string) error {
			return state.Disable()
		},
	})

	{
		dbCmd := &cobra.Command{
			Use:   "db",
			Short: "At-rest encryption for sqlite databases (mempalace.db, hermes.db). Wired into systemd ExecStartPre / ExecStopPost.",
		}
		dbCmd.AddCommand(&cobra.Command{
			Use:   "lock <path>",
			Short: "Encrypt <path> → <path>.age and remove the plaintext (idempotent)",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				return atrest.Lock(args[0])
			},
		})
		dbCmd.AddCommand(&cobra.Command{
			Use:   "unlock <path>",
			Short: "Decrypt <path>.age → <path> and remove the ciphertext (idempotent)",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				return atrest.Unlock(args[0])
			},
		})
		dbCmd.AddCommand(&cobra.Command{
			Use:   "lock-all",
			Short: "Lock both ~/.yashigatakae/{mempalace,hermes}.db (typical ExecStopPost)",
			RunE: func(c *cobra.Command, args []string) error {
				return atrest.LockAll(defaultDBPaths())
			},
		})
		dbCmd.AddCommand(&cobra.Command{
			Use:   "unlock-all",
			Short: "Unlock both ~/.yashigatakae/{mempalace,hermes}.db (typical ExecStartPre)",
			RunE: func(c *cobra.Command, args []string) error {
				return atrest.UnlockAll(defaultDBPaths())
			},
		})
		root.AddCommand(dbCmd)
	}

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newInitCmd() *cobra.Command {
	var vps, github, skipGstack bool
	var stateRepo string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Bootstrap this machine: gstack, caveman hooks, state render, MCP placeholder",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := state.InitOptions{
				VPS:            vps,
				GitHub:         github,
				LocalStateRepo: stateRepo,
				SkipGstack:     skipGstack,
			}
			return state.Run(opts)
		},
	}

	cmd.Flags().BoolVar(&vps, "vps", false, "Run VPS-side bootstrap (mempalace, hermes, bifrost, kintsugi relay)")
	cmd.Flags().BoolVar(&github, "github", false, "Create GitHub repos (state, memory-mirror)")
	cmd.Flags().StringVar(&stateRepo, "state-repo", "", "Path to local yashigatakae-state checkout (overrides github fetch)")
	cmd.Flags().BoolVar(&skipGstack, "skip-gstack", false, "Skip the gstack ./setup step (useful for dogfood / CI; gstack still expected to be installed separately)")

	return cmd
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Verify everything is wired correctly; print fixits",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doctor.Run()
		},
	}
}

func newCavemanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "caveman <lite|full|ultra|off> | <subcommand>",
		Short: "Caveman: compression level + auto-compaction + tool truncation + prompt cache",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return caveman.SetLevel(args[0])
		},
	}

	// caveman compact [--dry-run]
	{
		var dryRun bool
		var sessionID string
		var transcriptPath string
		sub := &cobra.Command{
			Use:   "compact",
			Short: "Show the compaction prompt that would inject at the configured threshold",
			RunE: func(c *cobra.Command, args []string) error {
				cfg, err := caveman.Load()
				if err != nil {
					return err
				}
				transcript := ""
				if transcriptPath != "" {
					b, err := os.ReadFile(transcriptPath)
					if err != nil {
						return err
					}
					transcript = string(b)
				}
				rep, err := caveman.CheckPressure(transcript, sessionID)
				if err != nil {
					return err
				}
				if dryRun {
					fmt.Printf("estimated_tokens=%d threshold=%d should_compact=%v\n",
						rep.EstimatedTokens, rep.Threshold, rep.ShouldCompact)
					if rep.ShouldCompact {
						fmt.Println("---")
						fmt.Print(rep.Prompt)
					}
					return nil
				}
				_ = cfg
				if rep.ShouldCompact {
					fmt.Print(rep.Prompt)
				}
				return nil
			},
		}
		sub.Flags().BoolVar(&dryRun, "dry-run", false, "print pressure stats and the prompt without exiting non-zero")
		sub.Flags().StringVar(&sessionID, "session", "", "session id to embed in the compaction header")
		sub.Flags().StringVar(&transcriptPath, "transcript", "", "path to a file whose size approximates current context (defaults to empty = no pressure)")
		cmd.AddCommand(sub)
	}

	// caveman truncate --tool Bash (reads stdin)
	{
		var tool string
		var asJSON bool
		sub := &cobra.Command{
			Use:   "truncate",
			Short: "Truncate stdin to the per-tool cap; full output spilled to /tmp/caveman/<sha8>.txt",
			RunE: func(c *cobra.Command, args []string) error {
				if tool == "" {
					return fmt.Errorf("--tool required (Bash, Read, Write, Edit, WebFetch, …)")
				}
				buf, err := readAllStdin()
				if err != nil {
					return err
				}
				res, err := caveman.TruncateForTool(tool, string(buf))
				if err != nil {
					return err
				}
				if asJSON {
					b, _ := json.MarshalIndent(res, "", "  ")
					fmt.Println(string(b))
					return nil
				}
				fmt.Print(res.Output)
				return nil
			},
		}
		sub.Flags().StringVar(&tool, "tool", "", "tool name (Bash, Read, Write, Edit, WebFetch)")
		sub.Flags().BoolVar(&asJSON, "json", false, "emit the full TruncateResult as JSON instead of just the truncated body")
		cmd.AddCommand(sub)
	}

	// caveman cache ephemeral
	{
		sub := &cobra.Command{
			Use:   "cache",
			Short: "Emit the cache_control marker the SessionStart hook embeds in the system prompt",
		}
		sub.AddCommand(&cobra.Command{
			Use:   "ephemeral",
			Short: "Print the JSON marker for an ephemeral cache_control entry (or null when disabled)",
			RunE: func(c *cobra.Command, args []string) error {
				b, err := caveman.PromptCacheMarker()
				if err != nil {
					return err
				}
				fmt.Println(string(b))
				return nil
			},
		})
		cmd.AddCommand(sub)
	}

	// caveman config get|set <key>=<value>
	{
		sub := &cobra.Command{
			Use:   "config",
			Short: "Inspect and edit ~/.yashigatakae/caveman.json",
		}
		sub.AddCommand(&cobra.Command{
			Use:   "get",
			Short: "Print the merged caveman config",
			RunE: func(c *cobra.Command, args []string) error {
				cfg, err := caveman.Load()
				if err != nil {
					return err
				}
				b, _ := json.MarshalIndent(cfg, "", "  ")
				fmt.Println(string(b))
				return nil
			},
		})
		sub.AddCommand(&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Update one config field (level, verbosity, compact_threshold_tokens, compact_target_tokens, default_tool_cap, prompt_cache_ephemeral, tool_caps.<Tool>=<n>)",
			Args:  cobra.ExactArgs(2),
			RunE: func(c *cobra.Command, args []string) error {
				cfg, err := caveman.Load()
				if err != nil {
					return err
				}
				if err := applyConfigSet(&cfg, args[0], args[1]); err != nil {
					return err
				}
				if err := caveman.Save(cfg); err != nil {
					return err
				}
				fmt.Printf("✓ %s=%s\n", args[0], args[1])
				return nil
			},
		})
		cmd.AddCommand(sub)
	}

	return cmd
}

func applyConfigSet(cfg *caveman.Config, key, value string) error {
	switch key {
	case "level":
		cfg.Level = value
	case "verbosity":
		cfg.Verbosity = value
	case "compact_threshold_tokens":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.CompactThresholdTokens = n
	case "compact_target_tokens":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.CompactTargetTokens = n
	case "default_tool_cap":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.DefaultToolCap = n
	case "prompt_cache_ephemeral":
		cfg.PromptCacheEphemeral = value == "true" || value == "1" || value == "yes"
	default:
		// tool_caps.Bash=4000 form
		const prefix = "tool_caps."
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			tool := key[len(prefix):]
			n, err := strconv.Atoi(value)
			if err != nil {
				return err
			}
			if cfg.ToolCaps == nil {
				cfg.ToolCaps = map[string]int{}
			}
			cfg.ToolCaps[tool] = n
			return nil
		}
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

func readAllStdin() ([]byte, error) {
	const max = 64 << 20 // 64 MiB hard cap; we're truncating, not archiving
	var out []byte
	buf := make([]byte, 64<<10)
	for len(out) < max {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return out, nil
}

func newStateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Manage the yashigatakae-state repo (skills/hooks/templates)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "render",
		Short: "Render templates from state repo into ~/.claude/",
		RunE: func(cmd *cobra.Command, args []string) error {
			return state.Render()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "pull",
		Short: "Git pull the state repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			return state.Pull()
		},
	})

	{
		var name, owner, template string
		var public, noClone, noWrite bool
		sub := &cobra.Command{
			Use:   "init",
			Short: "Create a new private state repo from the public template + wire STATE_REPO_URL",
			Long: `Creates a new GitHub repo (default private) from the public template
oyash01/yashigatakae-state-template under your account, clones it to
~/.yashigatakae/state, and writes STATE_REPO_URL into ~/.yashigatakae/secrets.env
so the next ` + "`yashigatakae init`" + ` picks it up automatically.

Requires the gh CLI (https://cli.github.com) installed and authenticated as
the user you want the repo created under.`,
			RunE: func(c *cobra.Command, args []string) error {
				return state.CreateStateRepo(state.CreateStateRepoOptions{
					Name:        name,
					Owner:       owner,
					Private:     !public,
					Template:    template,
					NoClone:     noClone,
					WriteSecret: !noWrite,
				})
			},
		}
		sub.Flags().StringVar(&name, "name", "yashigatakae-state", "Repo name (without owner)")
		sub.Flags().StringVar(&owner, "owner", "", "GitHub user/org to create under (default: gh-authenticated user)")
		sub.Flags().StringVar(&template, "template", "oyash01/yashigatakae-state-template", "Template repo (must be public or accessible)")
		sub.Flags().BoolVar(&public, "public", false, "Create as public (default private — recommended)")
		sub.Flags().BoolVar(&noClone, "no-clone", false, "Don't clone after creating")
		sub.Flags().BoolVar(&noWrite, "no-write-secret", false, "Don't append STATE_REPO_URL to secrets.env")
		cmd.AddCommand(sub)
	}

	return cmd
}

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage encrypted secrets.env synced via SSH from VPS",
	}
	{
		var keys []string
		var restart, dryRun bool
		sub := &cobra.Command{
			Use:   "rotate",
			Short: "Generate new random values for BIFROST_API_KEY + KINTSUGI_KEY (or specific --key) and write secrets.env",
			RunE: func(c *cobra.Command, args []string) error {
				return secrets.Rotate(secrets.RotateOptions{
					OnlyKeys:        keys,
					RestartServices: restart,
					DryRun:          dryRun,
				})
			},
		}
		sub.Flags().StringSliceVar(&keys, "key", nil, "Specific key(s) to rotate (default: BIFROST_API_KEY + KINTSUGI_KEY)")
		sub.Flags().BoolVar(&restart, "restart", false, "After write, systemctl restart all yashigatakae services (root only)")
		sub.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would change; don't modify secrets.env")
		cmd.AddCommand(sub)
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "pull",
		Short: "Pull ~/.yashigatakae/secrets.env from VPS",
		RunE: func(cmd *cobra.Command, args []string) error {
			return secrets.Pull()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "push",
		Short: "Push ~/.yashigatakae/secrets.env to VPS",
		RunE: func(cmd *cobra.Command, args []string) error {
			return secrets.Push()
		},
	})
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print machine cluster, sync state, drift, VPS health",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doctor.Status()
		},
	}
}

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Push/pull static state and diff memory across machines",
		RunE: func(cmd *cobra.Command, args []string) error {
			return state.Sync()
		},
	}
}

func newUpgradeCmd() *cobra.Command {
	var tag string
	var includeState bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Self-update yashigatakae from the latest GitHub release",
		RunE: func(c *cobra.Command, args []string) error {
			res, err := state.Upgrade(state.UpgradeOptions{
				TargetTag:    tag,
				IncludeState: includeState,
			})
			if err != nil {
				return err
			}
			fmt.Printf("\n✓ upgrade complete: %s → %s\n", res.OldVersion, res.NewVersion)
			fmt.Printf("  binary: %s\n", res.BinaryPath)
			if res.StateBumped {
				fmt.Println("  state-repo: pulled to latest")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "Pin a specific version (default: latest)")
	cmd.Flags().BoolVar(&includeState, "state", true, "Also git-pull the state-repo")
	return cmd
}

// defaultDBPaths returns the canonical paths for at-rest lock-all / unlock-all.
func defaultDBPaths() []string {
	yash, err := osdetect.YashigatakaeDir()
	if err != nil {
		return nil
	}
	return []string{
		fmt.Sprintf("%s/mempalace.db", yash),
		fmt.Sprintf("%s/hermes.db", yash),
	}
}

// runInteractive is what `yashigatakae` (no args) does. Opens the root TUI
// menu, then dispatches the chosen action by re-running the right subcommand
// in-process. Avoids importing every subsystem from internal/tui to keep the
// dependency graph one-way.
func runInteractive() error {
	for {
		action, err := tui.RunAndDispatch()
		if err != nil {
			return err
		}
		switch action {
		case "doctor":
			if err := doctor.Run(); err != nil {
				return err
			}
			fmt.Println()
		case "status":
			if err := doctor.Status(); err != nil {
				return err
			}
			fmt.Println()
		case "backfill":
			rep, err := kintsugi.Backfill(context.Background(), kintsugi.BackfillOptions{})
			if err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
			} else {
				fmt.Printf("✓ scanned=%d uploaded=%d skipped=%d failed=%d\n",
					rep.Scanned, rep.Uploaded, rep.Skipped, rep.Failed)
			}
			fmt.Println()
		case "sessions":
			if err := runSessionsBrowser(); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
			}
		case "mempalace", "hermes":
			fmt.Println("(coming in TUI v2 — for now use `yashigatakae", action, "--help`)")
		case "exit", "":
			return nil
		}
	}
}

// runSessionsBrowser merges local + relay sessions into one TUI list and
// dispatches the user's pick (resume / abandon).
func runSessionsBrowser() error {
	claudeDir, _ := osdetect.ClaudeDir()
	rows := tui.LoadLocalSessions(claudeDir)

	// Best-effort relay merge: tolerate missing env / unreachable relay.
	if cfg, err := kintsugi.ResolveEnvForCLI(); err == nil {
		client := kintsugi.NewClient(cfg.RelayBase, cfg.APIKey)
		if sids, err := client.ListSessions(context.Background()); err == nil {
			seen := map[string]int{}
			for i, r := range rows {
				seen[r.SessionID] = i
			}
			for _, sid := range sids {
				if i, ok := seen[sid]; ok {
					rows[i].OnRelay = true
				} else {
					rows = append(rows, tui.SessionRow{SessionID: sid, OnRelay: true})
				}
			}
		}
	}

	res, err := tui.RunSessionsBrowser(rows)
	if err != nil || res.Picked == nil {
		return err
	}
	if res.Action == "resume" {
		fmt.Printf("\nResuming %s ...\n", res.Picked.SessionID)
		_, err := kintsugi.Resume(context.Background(), kintsugi.ResumeOptions{
			SessionID: res.Picked.SessionID,
			TargetCWD: res.Picked.SourceCWD,
		})
		if err != nil {
			return err
		}
		fmt.Printf("\nclaude --continue %s\n", res.Picked.SessionID)
	}
	return nil
}

// notYet builds a placeholder cobra command that explains when a subsystem ships.
func notYet(name, milestone string, help func() string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("(stub — ships in %s) %s", milestone, name),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("[%s] not yet implemented — scheduled for %s.\n\n%s\n", name, milestone, help())
			return nil
		},
	}
}

func newMempalaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mempalace",
		Short: "Lifetime semantic memory store (remember / recall / forget / stats)",
	}

	// remember
	{
		var project, tags, source, sourceMachine string
		var noDedupe, noCategorize bool
		sub := &cobra.Command{
			Use:   "remember <text>",
			Short: "Store a memory entry (semantically embedded if API key is set)",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				body := joinArgs(args)
				id, err := mempalace.Remember(context.Background(), mempalace.RememberOptions{
					Body:          body,
					Source:        source,
					SourceMachine: sourceMachine,
					Project:       project,
					Tags:          tags,
					NoDedupe:      noDedupe,
					NoCategorize:  noCategorize,
				})
				if err != nil {
					return err
				}
				fmt.Printf("✓ remembered #%d\n", id)
				return nil
			},
		}
		sub.Flags().StringVar(&project, "project", "", "Project tag (e.g. ghostnode)")
		sub.Flags().StringVar(&tags, "tags", "", "Comma-separated tags")
		sub.Flags().StringVar(&source, "source", "cli", "Source label (cli, hook, hermes, etc.)")
		sub.Flags().StringVar(&sourceMachine, "source-machine", "", "Hostname of the machine the entry came from")
		sub.Flags().BoolVar(&noDedupe, "no-dedupe", false, "Disable the cosine/string near-duplicate check")
		sub.Flags().BoolVar(&noCategorize, "no-categorize", false, "Skip auto-category assignment")
		cmd.AddCommand(sub)
	}

	// recall
	{
		var top int
		var project, category, mode string
		var halfLifeDays float64
		var asJSON bool
		sub := &cobra.Command{
			Use:   "recall <query>",
			Short: "Search memory by hybrid (cosine + BM25, RRF-fused) ranking with time-decay",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				query := joinArgs(args)
				hits, err := mempalace.Recall(context.Background(), mempalace.RecallOptions{
					Query:        query,
					TopK:         top,
					Project:      project,
					Category:     category,
					HalfLifeDays: halfLifeDays,
					Mode:         mode,
				})
				if err != nil {
					return err
				}
				if asJSON {
					b, _ := json.MarshalIndent(hits, "", "  ")
					fmt.Println(string(b))
					return nil
				}
				if len(hits) == 0 {
					fmt.Println("(no hits)")
					return nil
				}
				for _, h := range hits {
					body := h.Body
					if len(body) > 200 {
						body = body[:200] + "…"
					}
					cat := h.Category
					if cat == "" {
						cat = "-"
					}
					fmt.Printf("  #%-5d  %.4f  [%s/%s/%s]  %s\n", h.ID, h.Score, h.Source, h.Project, cat, body)
				}
				return nil
			},
		}
		sub.Flags().IntVar(&top, "top", 10, "Max number of hits to return")
		sub.Flags().StringVar(&project, "project", "", "Limit search to one project")
		sub.Flags().StringVar(&category, "category", "", "Filter by category (user_pref|observation|fact|decision|error|code_snippet|url|lesson|misc)")
		sub.Flags().StringVar(&mode, "mode", "hybrid", "Ranker mode: hybrid|semantic|keyword")
		sub.Flags().Float64Var(&halfLifeDays, "half-life", 30, "Time-decay half-life in days (0 disables decay)")
		sub.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
		cmd.AddCommand(sub)
	}

	// consolidate
	{
		var project, window string
		var batchSize int
		var dryRun, archive bool
		sub := &cobra.Command{
			Use:   "consolidate",
			Short: "Roll up the oldest --batch entries in --window into a single summary entry; --archive removes originals",
			RunE: func(c *cobra.Command, args []string) error {
				w, err := time.ParseDuration(window)
				if err != nil {
					return fmt.Errorf("invalid --window: %w", err)
				}
				res, err := mempalace.Consolidate(context.Background(), mempalace.ConsolidateOptions{
					Project:   project,
					Window:    w,
					BatchSize: batchSize,
					DryRun:    dryRun,
					Archive:   archive,
				})
				if err != nil {
					return err
				}
				fmt.Printf("inspected=%d  summaries=%d  archived=%d\n", res.Inspected, res.Summaries, res.Archived)
				if res.ArchivePath != "" {
					fmt.Printf("archive: %s\n", res.ArchivePath)
				}
				if len(res.NewEntryIDs) > 0 {
					fmt.Printf("new summary ids: %v\n", res.NewEntryIDs)
				}
				return nil
			},
		}
		sub.Flags().StringVar(&project, "project", "", "Limit to one project")
		sub.Flags().StringVar(&window, "window", "720h", "Only consider entries OLDER than now-window (e.g. 720h = 30d)")
		sub.Flags().IntVar(&batchSize, "batch", 50, "Entries per summary")
		sub.Flags().BoolVar(&dryRun, "dry-run", false, "Print the summary without inserting / archiving")
		sub.Flags().BoolVar(&archive, "archive", false, "Move originals to ~/.yashigatakae/mempalace-archive/<ts>.jsonl and delete from active set")
		cmd.AddCommand(sub)
	}

	// forget
	{
		sub := &cobra.Command{
			Use:   "forget <id>",
			Short: "Delete a memory entry by ID",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				id, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return fmt.Errorf("id must be integer: %w", err)
				}
				ok, err := mempalace.Forget(context.Background(), id)
				if err != nil {
					return err
				}
				if !ok {
					fmt.Printf("? no entry with id=%d\n", id)
					return nil
				}
				fmt.Printf("✓ forgot #%d\n", id)
				return nil
			},
		}
		cmd.AddCommand(sub)
	}

	// stats
	{
		var asJSON bool
		sub := &cobra.Command{
			Use:   "stats",
			Short: "Show entry counts and store path",
			RunE: func(c *cobra.Command, args []string) error {
				stats, err := mempalace.Stats(context.Background())
				if err != nil {
					return err
				}
				if asJSON {
					b, _ := json.MarshalIndent(stats, "", "  ")
					fmt.Println(string(b))
					return nil
				}
				fmt.Printf("  path:           %s\n", stats.Path)
				fmt.Printf("  total_entries:  %d\n", stats.TotalEntries)
				fmt.Printf("  with_embedding: %d\n", stats.WithEmbedding)
				fmt.Printf("  size_bytes:     %d\n", stats.SizeBytes)
				if len(stats.Projects) > 0 {
					fmt.Printf("  projects:       %v\n", stats.Projects)
				}
				return nil
			},
		}
		sub.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
		cmd.AddCommand(sub)
	}

	// serve — HTTP MCP server
	{
		var addr string
		sub := &cobra.Command{
			Use:   "serve",
			Short: "Run mempalace as an HTTP MCP server (recall / remember / forget / stats tools)",
			RunE: func(c *cobra.Command, args []string) error {
				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				return mempalace.Serve(ctx, addr)
			},
		}
		sub.Flags().StringVar(&addr, "addr", "127.0.0.1:8765", "HTTP listen address")
		cmd.AddCommand(sub)
	}

	return cmd
}

func newHermesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hermes",
		Short: "Background self-learning agent (queue / serve / ls / logs / cancel)",
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Println(hermes.Help())
			return nil
		},
	}

	{
		var project, cwd, prompt, note, idemKey string
		var priority, maxRetries int
		var dependsOn int64
		sub := &cobra.Command{
			Use:   "enqueue",
			Short: "Add a Claude task to the queue",
			RunE: func(c *cobra.Command, args []string) error {
				if prompt == "" && len(args) > 0 {
					prompt = joinArgs(args)
				}
				id, hit, err := hermes.Enqueue(context.Background(), hermes.Task{
					Project:        project,
					CWD:            cwd,
					Prompt:         prompt,
					Note:           note,
					Priority:       priority,
					MaxRetries:     maxRetries,
					IdempotencyKey: idemKey,
					DependencyID:   dependsOn,
				})
				if err != nil {
					return err
				}
				if hit {
					fmt.Printf("· dedupe hit on idempotency-key %q → existing task #%d\n", idemKey, id)
				} else {
					fmt.Printf("✓ enqueued #%d project=%s priority=%d\n", id, project, priority)
				}
				return nil
			},
		}
		sub.Flags().StringVar(&project, "project", "", "Project label (required)")
		sub.Flags().StringVar(&cwd, "cwd", "", "Working dir for `claude -p`")
		sub.Flags().StringVar(&prompt, "prompt", "", "Prompt text (or pass as positional args)")
		sub.Flags().StringVar(&note, "note", "", "Free-text note saved on the task row")
		sub.Flags().StringVar(&idemKey, "idempotency-key", "", "Dedupe key — same key within 7d returns the existing task id")
		sub.Flags().IntVar(&priority, "priority", 5, "Higher number = picked first (1..10)")
		sub.Flags().IntVar(&maxRetries, "max-retries", 5, "Attempt cap before the task lands in the DLQ")
		sub.Flags().Int64Var(&dependsOn, "depends-on", 0, "Task id that must reach status=done before this one is eligible")
		cmd.AddCommand(sub)
	}

	{
		dlq := &cobra.Command{
			Use:   "dlq",
			Short: "Inspect tasks that exhausted retries",
		}
		dlq.AddCommand(&cobra.Command{
			Use:   "ls",
			Short: "List dead-lettered tasks",
			RunE: func(c *cobra.Command, args []string) error {
				tasks, err := hermes.List(context.Background(), "dlq", 100)
				if err != nil {
					return err
				}
				if len(tasks) == 0 {
					fmt.Println("(no DLQ tasks)")
					return nil
				}
				for _, t := range tasks {
					reason := t.DLQReason
					if len(reason) > 60 {
						reason = reason[:60] + "…"
					}
					fmt.Printf("  #%-5d  attempts=%d/%d  project=%-12s  %s\n", t.ID, t.RetryCount, t.MaxRetries, t.Project, reason)
				}
				return nil
			},
		})
		dlq.AddCommand(&cobra.Command{
			Use:   "retry <id>",
			Short: "Reset retry_count and put a DLQ task back on pending",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				id, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return err
				}
				if err := hermes.RetryDLQ(context.Background(), id); err != nil {
					return err
				}
				fmt.Printf("✓ task #%d requeued from DLQ\n", id)
				return nil
			},
		})
		cmd.AddCommand(dlq)
	}

	{
		sched := &cobra.Command{
			Use:   "schedule",
			Short: "Manage cron-style recurring tasks",
		}
		{
			var project, prompt, cwd, note string
			var priority, maxRetries int
			add := &cobra.Command{
				Use:   "add <cron>",
				Short: "Add a cron schedule. Cron is 5 fields: m h dom mon dow",
				Args:  cobra.ExactArgs(1),
				RunE: func(c *cobra.Command, args []string) error {
					if _, err := hermes.ParseCron(args[0]); err != nil {
						return err
					}
					id, err := hermes.AddSchedule(context.Background(), hermes.Schedule{
						Cron:       args[0],
						Project:    project,
						Prompt:     prompt,
						CWD:        cwd,
						Note:       note,
						Priority:   priority,
						MaxRetries: maxRetries,
					})
					if err != nil {
						return err
					}
					fmt.Printf("✓ schedule #%d created (%s)\n", id, args[0])
					return nil
				},
			}
			add.Flags().StringVar(&project, "project", "", "Project label (required)")
			add.Flags().StringVar(&prompt, "prompt", "", "Prompt text (required)")
			add.Flags().StringVar(&cwd, "cwd", "", "Working dir")
			add.Flags().StringVar(&note, "note", "", "Free-text note")
			add.Flags().IntVar(&priority, "priority", 5, "Priority for fired tasks")
			add.Flags().IntVar(&maxRetries, "max-retries", 5, "Retry cap on fired tasks")
			sched.AddCommand(add)
		}
		sched.AddCommand(&cobra.Command{
			Use:   "ls",
			Short: "List active schedules",
			RunE: func(c *cobra.Command, args []string) error {
				scs, err := hermes.ListSchedules(context.Background())
				if err != nil {
					return err
				}
				if len(scs) == 0 {
					fmt.Println("(no schedules)")
					return nil
				}
				for _, sc := range scs {
					last := "(never)"
					if !sc.LastFiredAt.IsZero() {
						last = sc.LastFiredAt.Format(time.RFC3339)
					}
					fmt.Printf("  #%-3d  %-15s  project=%-12s  last=%s\n", sc.ID, sc.Cron, sc.Project, last)
				}
				return nil
			},
		})
		sched.AddCommand(&cobra.Command{
			Use:   "rm <id>",
			Short: "Delete a schedule",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				id, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return err
				}
				if err := hermes.DeleteSchedule(context.Background(), id); err != nil {
					return err
				}
				fmt.Printf("✓ schedule #%d deleted\n", id)
				return nil
			},
		})
		cmd.AddCommand(sched)
	}

	{
		var status string
		var limit int
		sub := &cobra.Command{
			Use:   "ls",
			Short: "List tasks (newest first)",
			RunE: func(c *cobra.Command, args []string) error {
				tasks, err := hermes.List(context.Background(), status, limit)
				if err != nil {
					return err
				}
				if len(tasks) == 0 {
					fmt.Println("(no tasks)")
					return nil
				}
				for _, t := range tasks {
					p := t.Prompt
					if len(p) > 60 {
						p = p[:60] + "…"
					}
					fmt.Printf("  #%-5d  %-9s  %-12s  %s\n", t.ID, t.Status, t.Project, p)
				}
				return nil
			},
		}
		sub.Flags().StringVar(&status, "status", "", "Filter: pending|running|done|failed|cancelled|dlq|scheduled")
		sub.Flags().IntVar(&limit, "limit", 50, "Max rows")
		cmd.AddCommand(sub)
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "logs <id>",
		Short: "Stream the log for a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return err
			}
			return hermes.TailLogs(context.Background(), id, os.Stdout)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "cancel <id>",
		Short: "Mark a pending or running task as cancelled",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return err
			}
			ok, err := hermes.Cancel(context.Background(), id)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Printf("? task #%d not in pending/running\n", id)
			} else {
				fmt.Printf("✓ cancelled #%d\n", id)
			}
			return nil
		},
	})

	{
		var poll, claudeBin, baseBackoff, maxBackoff string
		var noLessons bool
		var concurrency int
		sub := &cobra.Command{
			Use:   "serve",
			Short: "Run the worker loop (foreground; systemd unit on VPS)",
			RunE: func(c *cobra.Command, args []string) error {
				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				d, err := time.ParseDuration(poll)
				if err != nil {
					return fmt.Errorf("invalid --poll: %w", err)
				}
				bb, err := time.ParseDuration(baseBackoff)
				if err != nil {
					return fmt.Errorf("invalid --base-backoff: %w", err)
				}
				mb, err := time.ParseDuration(maxBackoff)
				if err != nil {
					return fmt.Errorf("invalid --max-backoff: %w", err)
				}
				return hermes.Serve(ctx, hermes.WorkerOptions{
					PollInterval: d,
					ClaudeBin:    claudeBin,
					WriteLessons: !noLessons,
					Concurrency:  concurrency,
					BaseBackoff:  bb,
					MaxBackoff:   mb,
				})
			},
		}
		sub.Flags().StringVar(&poll, "poll", "5s", "How often to check for new tasks")
		sub.Flags().StringVar(&claudeBin, "claude", "claude", "Path to the claude binary")
		sub.Flags().BoolVar(&noLessons, "no-lessons", false, "Skip writing the final output as a mempalace lesson")
		sub.Flags().IntVar(&concurrency, "concurrency", 1, "How many parallel claude subprocesses (sqlite WAL handles modest contention; >4 not recommended)")
		sub.Flags().StringVar(&baseBackoff, "base-backoff", "30s", "First retry delay; subsequent retries multiply by 10× (capped by --max-backoff)")
		sub.Flags().StringVar(&maxBackoff, "max-backoff", "8h", "Cap on retry delay")
		cmd.AddCommand(sub)
	}

	return cmd
}

func newGraphifyCmd() *cobra.Command {
	var refresh, pro bool
	var outDir string
	cmd := &cobra.Command{
		Use:   "graphify <repo>",
		Short: "Generate a Karpathy LLM Wiki under ~/.yashigatakae/state/codebase-wiki/",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			res, err := graphify.Run(graphify.Options{
				Repo:    args[0],
				Refresh: refresh,
				OutDir:  outDir,
				Pro:     pro,
			})
			if err != nil {
				return err
			}
			fmt.Printf("✓ wiki at %s\n", res.WikiDir)
			fmt.Printf("  files: %d   bytes: %d   HEAD: %s\n", res.Files, res.Bytes, res.GitCommit)
			return nil
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Force regenerate even if recent")
	cmd.Flags().StringVar(&outDir, "out", "", "Override output dir")
	cmd.Flags().BoolVar(&pro, "pro", true, "Generate the full Karpathy LLM Wiki taxonomy (modules/, symbols/, DECISIONS, GLOSSARY, STUB-PAGES, _meta/citations.json)")
	cmd.AddCommand(&cobra.Command{
		Use:   "check <repo>",
		Short: "Exit non-zero if STUB-PAGES.md has any unresolved [[wikilinks]]",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			n, err := graphify.CheckWiki(graphify.Options{Repo: args[0], OutDir: outDir})
			if err != nil {
				return err
			}
			if n > 0 {
				return fmt.Errorf("%d unresolved wikilinks (see STUB-PAGES.md)", n)
			}
			fmt.Println("✓ wiki has no broken wikilinks")
			return nil
		},
	})
	return cmd
}

func newBackfillCmd() *cobra.Command {
	var dryRun, force, skipDisk bool
	var limit int
	var sinceStr string
	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Encrypt + upload every transcript under ~/.claude/projects/ to the kintsugi relay (idempotent via ~/.yashigatakae/backfill.json ledger)",
		RunE: func(c *cobra.Command, args []string) error {
			ctx := context.Background()
			var since time.Duration
			if sinceStr != "" {
				d, err := time.ParseDuration(sinceStr)
				if err != nil {
					return fmt.Errorf("invalid --since: %w", err)
				}
				since = d
			}
			rep, err := kintsugi.Backfill(ctx, kintsugi.BackfillOptions{
				DryRun:        dryRun,
				Limit:         limit,
				Since:         since,
				Force:         force,
				SkipDiskCheck: skipDisk,
			})
			if err != nil {
				return err
			}
			fmt.Printf("\n✓ backfill complete\n")
			fmt.Printf("  scanned=%d uploaded=%d skipped=%d failed=%d\n", rep.Scanned, rep.Uploaded, rep.Skipped, rep.Failed)
			fmt.Printf("  bytes_in=%d bytes_out=%d duration=%s\n", rep.BytesIn, rep.BytesOut, rep.FinishedAt.Sub(rep.StartedAt).Round(time.Millisecond))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "List what would be uploaded; don't push")
	cmd.Flags().IntVar(&limit, "limit", 0, "Cap the number of transcripts uploaded (0 = unlimited)")
	cmd.Flags().StringVar(&sinceStr, "since", "", "Only consider transcripts modified within this duration (e.g. 720h for 30d)")
	cmd.Flags().BoolVar(&force, "force", false, "Re-upload even if ledger says it's already there")
	cmd.Flags().BoolVar(&skipDisk, "skip-disk-check", false, "Bypass relay disk-free pre-check (not recommended)")
	return cmd
}

func newHandoffCmd() *cobra.Command {
	var note string
	var includeMemo, includeWorktree, dryRun bool
	cmd := &cobra.Command{
		Use:   "handoff",
		Short: "Checkpoint the active Claude Code session to the kintsugi relay (resume on another machine)",
		RunE: func(c *cobra.Command, args []string) error {
			ctx := context.Background()
			code, err := kintsugi.Handoff(ctx, kintsugi.HandoffOptions{
				Note:            note,
				IncludeMemo:     includeMemo,
				IncludeWorktree: includeWorktree,
				DryRun:          dryRun,
			})
			if err != nil {
				return err
			}
			fmt.Printf("\n✓ handoff complete\n  resume code: %s\n", code)
			fmt.Println("\nOn the target machine:")
			fmt.Println("  yashigatakae resume                # picks latest")
			return nil
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "Optional note saved with the checkpoint")
	cmd.Flags().BoolVar(&includeMemo, "memory", true, "Pack memory dir + MEMORY.md + subagents + todos (default true)")
	cmd.Flags().BoolVar(&includeWorktree, "worktree", true, "Pack git diff + untracked files for the active cwd (default true)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Pack + encrypt + print size; do NOT upload")
	return cmd
}

func newResumeCmd() *cobra.Command {
	var sid, targetCWD string
	var dryRun, auto bool
	cmd := &cobra.Command{
		Use:   "resume [session-id]",
		Short: "Pull the latest kintsugi checkpoint and restore it to this machine",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 1 {
				sid = args[0]
			}
			ctx := context.Background()
			rep, err := kintsugi.Resume(ctx, kintsugi.ResumeOptions{
				SessionID: sid,
				TargetCWD: targetCWD,
				DryRun:    dryRun,
				Auto:      auto,
			})
			if err != nil {
				return err
			}
			fmt.Printf("\n✓ resumed session %s\n", rep.Manifest.SessionID)
			fmt.Printf("  source:     %s on %s\n", rep.Manifest.SourceCWD, rep.Manifest.SourceMachine)
			fmt.Printf("  transcript: %s\n", rep.TranscriptPath)
			if rep.MemoryRestored {
				fmt.Printf("  memory:     restored\n")
			}
			fmt.Printf("\nContinue the conversation:\n  claude --continue %s\n", rep.Manifest.SessionID)
			return nil
		},
	}
	cmd.Flags().StringVar(&targetCWD, "cwd", "", "Override target working directory (default = source CWD from manifest)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be restored, don't write files")
	cmd.Flags().BoolVar(&auto, "auto", false, "After restore, also exec `claude --continue <id>`")
	return cmd
}

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List / inspect synced sessions on the kintsugi relay",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List all session IDs available on the relay",
		RunE: func(c *cobra.Command, args []string) error {
			ctx := context.Background()
			cfg, err := kintsugi.ResolveEnvForCLI()
			if err != nil {
				return err
			}
			cl := kintsugi.NewClient(cfg.RelayBase, cfg.APIKey)
			sids, err := cl.ListSessions(ctx)
			if err != nil {
				return err
			}
			if len(sids) == 0 {
				fmt.Println("(no sessions)")
				return nil
			}
			for _, s := range sids {
				cps, _ := cl.ListCheckpoints(ctx, s)
				latest := ""
				if len(cps) > 0 {
					latest = cps[len(cps)-1].TS
				}
				fmt.Printf("  %s  %d checkpoint(s)  latest=%s\n", s, len(cps), latest)
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "checkpoints <session-id>",
		Short: "List checkpoints for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := context.Background()
			cfg, err := kintsugi.ResolveEnvForCLI()
			if err != nil {
				return err
			}
			cl := kintsugi.NewClient(cfg.RelayBase, cfg.APIKey)
			cps, err := cl.ListCheckpoints(ctx, args[0])
			if err != nil {
				return err
			}
			for _, cp := range cps {
				fmt.Printf("  %s  machine=%s  size=%d\n", cp.TS, cp.Machine, cp.Size)
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "abandon <session-id>",
		Short: "Delete a session and all its checkpoints from the relay",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Printf("(not yet — direct delete via curl: curl -X DELETE -H \"Authorization: Bearer $BIFROST_API_KEY\" $KINTSUGI_URL/sessions/%s)\n", args[0])
			return nil
		},
	})
	return cmd
}

func newKintsugiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kintsugi",
		Short: "Cross-device session+worktree continuity (handoff / resume / serve)",
	}
	{
		var addr, dataDir, apiKey, tlsDomain string
		var tlsOn bool
		sub := &cobra.Command{
			Use:   "serve",
			Short: "Run the kintsugi relay HTTP server (VPS-side)",
			RunE: func(c *cobra.Command, args []string) error {
				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				if apiKey == "" {
					apiKey = os.Getenv("BIFROST_API_KEY")
				}
				return kintsugi.ServeRelay(ctx, kintsugi.RelayConfig{
					Listen:     addr,
					APIKey:     apiKey,
					DataDir:    dataDir,
					TLSEnabled: tlsOn || tlsDomain != "",
					TLSDomain:  tlsDomain,
				})
			},
		}
		sub.Flags().StringVar(&addr, "addr", "127.0.0.1:8444", "HTTPS listen address")
		sub.Flags().StringVar(&dataDir, "data", "", "Override data dir (default ~/.yashigatakae/kintsugi)")
		sub.Flags().StringVar(&apiKey, "api-key", "", "Require Bearer auth (also reads BIFROST_API_KEY)")
		sub.Flags().StringVar(&tlsDomain, "tls-domain", "", "Public DNS domain for Let's Encrypt cert (requires :80 reachable + DNS A record). Empty = self-signed if --tls.")
		sub.Flags().BoolVar(&tlsOn, "tls", false, "Enable TLS with a self-signed cert (default off; use --tls-domain for real Let's Encrypt)")
		cmd.AddCommand(sub)
	}
	return cmd
}

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Hook entrypoints invoked by Claude Code (sweep, autocommit)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "sweep",
		Short: "SessionEnd: parse the just-finished transcript and remember each user/assistant pair",
		RunE: func(c *cobra.Command, args []string) error {
			yashihooks.RunSweepCmd()
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "autocommit",
		Short: "PostToolUse: rsync ~/.claude into state-repo working copy and auto-commit",
		RunE: func(c *cobra.Command, args []string) error {
			yashihooks.RunAutocommit()
			return nil
		},
	})
	return cmd
}

func newBifrostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bifrost",
		Short: "MCP gateway: one endpoint that fans out to N downstream MCP servers",
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Println(bifrost.Help())
			return nil
		},
	}

	{
		var addr, mempalaceURL, apiKey, tlsDomain string
		var tlsOn bool
		sub := &cobra.Command{
			Use:   "serve",
			Short: "Run the bifrost gateway HTTP server",
			RunE: func(c *cobra.Command, args []string) error {
				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				if apiKey == "" {
					apiKey = os.Getenv("BIFROST_API_KEY")
				}
				return bifrost.Serve(ctx, bifrost.Config{
					Listen: addr,
					APIKey: apiKey,
					Downstreams: []bifrost.Downstream{
						{Name: "mempalace", URL: mempalaceURL},
					},
					TLSEnabled: tlsOn || tlsDomain != "",
					TLSDomain:  tlsDomain,
				})
			},
		}
		sub.Flags().StringVar(&addr, "addr", "127.0.0.1:8443", "HTTPS listen address")
		sub.Flags().StringVar(&mempalaceURL, "mempalace", "http://127.0.0.1:8765/mcp", "mempalace MCP endpoint to proxy to")
		sub.Flags().StringVar(&apiKey, "api-key", "", "Require Bearer token on incoming requests (also reads BIFROST_API_KEY env)")
		sub.Flags().StringVar(&tlsDomain, "tls-domain", "", "Public DNS domain for Let's Encrypt cert (requires :80 reachable + DNS A record). Empty = self-signed if --tls.")
		sub.Flags().BoolVar(&tlsOn, "tls", false, "Enable TLS with a self-signed cert (default off; use --tls-domain for real Let's Encrypt)")
		cmd.AddCommand(sub)
	}

	return cmd
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
