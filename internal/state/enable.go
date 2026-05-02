package state

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// units is the canonical list of yashigatakae systemd services that should
// run perpetually on a VPS once `init --vps` lands them.
var units = []string{
	"yashigatakae-mempalace.service",
	"yashigatakae-bifrost.service",
	"yashigatakae-kintsugi.service",
	"yashigatakae-hermes.service",
}

// Enable wires the yashigatakae background services so they survive reboots,
// crashes, and OS package upgrades. Linux/systemd: enable + start every unit.
// On Mac/Win there are no client-side daemons yet (kintsugi watcher arrives
// later); the command becomes a no-op with a friendly note.
func Enable() error {
	switch runtime.GOOS {
	case "linux":
		return enableLinux(true)
	case "darwin", "windows":
		fmt.Printf("yashigatakae enable: no client-side daemons on %s yet (the always-on services live on the VPS).\n", runtime.GOOS)
		fmt.Println("  Run `yashigatakae init --vps` on your VPS for the bifrost / kintsugi / mempalace / hermes systemd units.")
		return nil
	default:
		return fmt.Errorf("yashigatakae enable: unsupported OS %q", runtime.GOOS)
	}
}

// Disable stops + disables every yashigatakae service (Linux only). Use it
// when intentionally winding down — the services were configured to restart
// indefinitely on crash, so this is the only clean way to stop them.
func Disable() error {
	switch runtime.GOOS {
	case "linux":
		return enableLinux(false)
	case "darwin", "windows":
		fmt.Printf("yashigatakae disable: nothing client-side to disable on %s.\n", runtime.GOOS)
		return nil
	default:
		return fmt.Errorf("yashigatakae disable: unsupported OS %q", runtime.GOOS)
	}
}

func enableLinux(on bool) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root (try: sudo yashigatakae enable)")
	}
	verbs := []string{"enable", "--now"}
	if !on {
		verbs = []string{"disable", "--now"}
	}
	args := append(verbs, units...)
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl %v: %w", verbs, err)
	}

	fmt.Println()
	fmt.Println("Status snapshot:")
	for _, u := range units {
		out, _ := exec.Command("systemctl", "is-active", u).Output()
		state := "?"
		if len(out) > 0 {
			state = string(out[:len(out)-1])
		}
		mark := "✓"
		if state != "active" {
			mark = "✗"
		}
		fmt.Printf("  %s %-40s %s\n", mark, u, state)
	}
	fmt.Println()
	fmt.Println("Tuning baked into every unit (won't stop until you `yashigatakae disable`):")
	fmt.Println("  Restart=always  RestartSec=3  StartLimitIntervalSec=0")
	fmt.Println("  TimeoutStopSec=10  KillSignal=SIGTERM")
	return nil
}
