package modules

import (
	"fmt"
	"strings"

	"github.com/wakeoneself/Baselock/internal/backup"
	"github.com/wakeoneself/Baselock/internal/sys"
)

type Updates struct{}

func (Updates) Name() string { return "updates" }

func (Updates) Apply(ctx Context) error {
	if ctx.DryRun {
		ctx.UI.Detail("would apt install unattended-upgrades apt-listchanges")
		ctx.UI.Detail("would enable unattended-upgrades")
		return nil
	}
	if err := sys.RunOK("apt-get", "install", "-y", "unattended-upgrades", "apt-listchanges"); err != nil {
		return fmt.Errorf("install unattended-upgrades: %w", err)
	}
	_ = sys.RunOK("dpkg-reconfigure", "-f", "noninteractive", "unattended-upgrades")
	if sys.CommandExists("systemctl") {
		_ = sys.RunOK("systemctl", "enable", "--now", "unattended-upgrades")
	}
	ctx.UI.Detail("unattended-upgrades enabled")
	return nil
}

func (Updates) Status() Check {
	if !sys.HostInfo().Debian {
		return skip("updates", "not Ubuntu/Debian")
	}
	if !sys.CommandExists("unattended-upgrade") && !sys.FileExists("/usr/bin/unattended-upgrade") {
		return fail("updates", "unattended-upgrades not installed", "run: sudo sec --updates")
	}
	if sys.CommandExists("systemctl") {
		out, err := sys.Run("systemctl", "is-enabled", "unattended-upgrades")
		if err != nil || (strings.TrimSpace(out) != "enabled" && strings.TrimSpace(out) != "static") {
			return warn("updates", "installed, service not enabled", "run: sudo sec --updates")
		}
	}
	return ok("updates", "unattended-upgrades enabled")
}

func (Updates) Revert(ctx Context, snap *backup.Snapshot) error {
	ctx.UI.Detail("updates module does not undo package installs")
	return nil
}
