// Command yashigatakae is the Claude Code orchestrator + lifetime memory + cross-device continuity tool.
// See https://github.com/oyash01/yashigatakae for the full design.
package main

import (
	"fmt"
	"os"

	"github.com/oyash01/yashigatakae/internal/bifrost"
	"github.com/oyash01/yashigatakae/internal/caveman"
	"github.com/oyash01/yashigatakae/internal/doctor"
	"github.com/oyash01/yashigatakae/internal/graphify"
	"github.com/oyash01/yashigatakae/internal/hermes"
	"github.com/oyash01/yashigatakae/internal/kintsugi"
	"github.com/oyash01/yashigatakae/internal/mempalace"
	"github.com/oyash01/yashigatakae/internal/secrets"
	"github.com/oyash01/yashigatakae/internal/state"
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
	}

	root.AddCommand(newInitCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newCavemanCmd())
	root.AddCommand(newStateCmd())
	root.AddCommand(newSecretsCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newUpgradeCmd())

	// v0.2+ — stubs that print a friendly "not yet" message
	root.AddCommand(notYet("mempalace", "v0.2", mempalace.Help))
	root.AddCommand(notYet("bifrost", "v0.2", bifrost.Help))
	root.AddCommand(notYet("graphify", "v0.4", graphify.Help))
	root.AddCommand(notYet("hermes", "v0.5", hermes.Help))
	root.AddCommand(notYet("handoff", "v0.3", kintsugi.HelpHandoff))
	root.AddCommand(notYet("resume", "v0.3", kintsugi.HelpResume))
	root.AddCommand(notYet("sessions", "v0.3", kintsugi.HelpSessions))
	root.AddCommand(notYet("link", "v0.6", state.HelpLink))

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
		Use:   "caveman <lite|full|ultra|off>",
		Short: "Switch caveman compression level",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return caveman.SetLevel(args[0])
		},
	}
	return cmd
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
	return cmd
}

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage encrypted secrets.env synced via SSH from VPS",
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
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Self-update yashigatakae, then upgrade gstack and skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("upgrade: ships in v0.6 — for now, re-run install.sh / install.ps1")
			return nil
		},
	}
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
