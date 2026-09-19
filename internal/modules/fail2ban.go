package modules

import (
	"fmt"
	"strings"

	"github.com/abyss/server-sec-cli/internal/backup"
	"github.com/abyss/server-sec-cli/internal/sys"
)

const fail2banJail = "/etc/fail2ban/jail.local"

const fail2banJailContents = `[sshd]
enabled = true
mode = aggressive
bantime = 1h
findtime = 10m
maxretry = 5
`

type Fail2ban struct{}

func (Fail2ban) Name() string { return "fail2ban" }

func (Fail2ban) Apply(ctx Context) error {
	if ctx.Snapshot != nil {
		_ = ctx.Snapshot.SaveFile(fail2banJail)
	}
	if ctx.DryRun {
		ctx.UI.Detail("would apt install fail2ban")
		ctx.UI.Detail("would write " + fail2banJail)
		return nil
	}
	if err := sys.RunOK("apt-get", "install", "-y", "fail2ban"); err != nil {
		return fmt.Errorf("install fail2ban: %w", err)
	}
	if err := sys.WriteFile(fail2banJail, []byte(fail2banJailContents), 0o644); err != nil {
		return err
	}
	if err := sys.RunOK("systemctl", "enable", "--now", "fail2ban"); err != nil {
		return fmt.Errorf("enable fail2ban: %w", err)
	}
	ctx.UI.Detail("fail2ban jail sshd · aggressive")
	return nil
}

func (Fail2ban) Status() Check {
	if !sys.HostInfo().Debian {
		return skip("fail2ban", "not Ubuntu/Debian")
	}
	if !sys.CommandExists("fail2ban-client") && !sys.CommandExists("fail2ban-server") {
		return fail("fail2ban", "not installed", "run: sudo sec --fail2ban")
	}
	out, err := sys.Run("systemctl", "is-active", "fail2ban")
	if err != nil || strings.TrimSpace(out) != "active" {
		return warn("fail2ban", "installed but not active", "systemctl enable --now fail2ban")
	}
	return ok("fail2ban", "active · sshd jail")
}

func (Fail2ban) Revert(ctx Context, snap *backup.Snapshot) error {
	if snap == nil {
		return fmt.Errorf("no snapshot")
	}
	if ctx.DryRun {
		ctx.UI.Detail("would restore " + fail2banJail)
		return nil
	}
	if err := snap.RestoreFile(fail2banJail); err != nil {
		return err
	}
	if sys.CommandExists("systemctl") {
		_ = sys.RunOK("systemctl", "restart", "fail2ban")
	}
	return nil
}
