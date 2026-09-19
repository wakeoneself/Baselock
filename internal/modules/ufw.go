package modules

import (
	"fmt"
	"os"
	"strings"

	"github.com/wakeoneself/Baselock/internal/backup"
	"github.com/wakeoneself/Baselock/internal/sys"
)

const ufwDockerBin = "/usr/local/bin/ufw-docker"

type UFW struct{}

func (UFW) Name() string { return "ufw" }

func (UFW) Apply(ctx Context) error {
	if ctx.Snapshot != nil {
		if out, err := sys.Run("ufw", "status", "verbose"); err == nil {
			_ = ctx.Snapshot.SaveNote("ufw-status.txt", []byte(out))
		}
		_ = ctx.Snapshot.SaveFile("/etc/ufw/after.rules")
	}

	sshPort := sys.DetectSSHPort()
	if ctx.DryRun {
		ctx.UI.Detail("would apt install ufw")
		ctx.UI.Detail("would default deny incoming / allow outgoing")
		ctx.UI.Detail(fmt.Sprintf("would allow %s/tcp, 80/tcp, 443/tcp", sshPort))
		ctx.UI.Detail("would enable UFW, then install ufw-docker (install requires UFW to be active)")
		return nil
	}

	if err := sys.RunOK("apt-get", "update", "-qq"); err != nil {
		ctx.UI.Detail("apt-get update failed, continuing if ufw is already installed")
	}
	if err := sys.RunOK("apt-get", "install", "-y", "ufw"); err != nil {
		return fmt.Errorf("install ufw: %w", err)
	}

	_ = sys.RunOK("ufw", "--force", "default", "deny", "incoming")
	_ = sys.RunOK("ufw", "--force", "default", "allow", "outgoing")
	if err := sys.RunOK("ufw", "allow", sshPort+"/tcp"); err != nil {
		return err
	}
	_ = sys.RunOK("ufw", "allow", "80/tcp")
	_ = sys.RunOK("ufw", "allow", "443/tcp")

	// ufw-docker install refuses to run while UFW is inactive.
	if err := sys.RunOK("ufw", "--force", "enable"); err != nil {
		return fmt.Errorf("enable ufw: %w", err)
	}
	ctx.UI.Detail("UFW active · SSH/" + sshPort + " · 80 · 443")

	if err := installUfwDocker(ctx); err != nil {
		ctx.UI.Detail("ufw-docker: " + err.Error())
		ctx.UI.Warn("UFW is on, but ufw-docker could not install — Docker-published ports may still bypass UFW")
		return nil
	}
	return nil
}

func installUfwDocker(ctx Context) error {
	if !sys.FileExists(ufwDockerBin) {
		if err := sys.RunOK("wget", "-q", "-O", ufwDockerBin,
			"https://github.com/chaifeng/ufw-docker/raw/master/ufw-docker"); err != nil {
			return err
		}
		if err := os.Chmod(ufwDockerBin, 0o755); err != nil {
			return err
		}
	}
	if err := sys.RunOK(ufwDockerBin, "install"); err != nil {
		return err
	}
	// Re-assert SSH after ufw-docker rewrites after.rules. Reload, don't restart.
	sshPort := sys.DetectSSHPort()
	_ = sys.RunOK("ufw", "allow", sshPort+"/tcp")
	_ = sys.RunOK("ufw", "reload")
	if err := sys.EnsureSSHListening(); err != nil {
		return fmt.Errorf("sshd not listening after ufw-docker: %w", err)
	}
	ctx.UI.Detail("installed " + ufwDockerBin)
	return nil
}

func (UFW) Status() Check {
	if !sys.HostInfo().Debian {
		return skip("ufw", "not Ubuntu/Debian")
	}
	if !sys.CommandExists("ufw") {
		return fail("ufw", "ufw is not installed", "run: sudo sec --ufw")
	}
	out, err := sys.RunPrivileged("ufw", "status")
	if err != nil {
		if !sys.IsRoot() {
			return warn("ufw", "needs root to read UFW", "run: sudo sec status")
		}
		return fail("ufw", err.Error(), "run: sudo sec --ufw")
	}
	active := strings.Contains(strings.ToLower(out), "status: active")
	if !active {
		return fail("ufw", "ufw is inactive", "run: sudo sec --ufw")
	}
	if !sys.FileExists(ufwDockerBin) {
		return warn("ufw", "active, but ufw-docker is missing (Docker can bypass UFW)", "run: sudo sec --ufw")
	}
	return ok("ufw", "active · SSH/80/443 · ufw-docker")
}

func (UFW) Revert(ctx Context, snap *backup.Snapshot) error {
	if snap == nil {
		return fmt.Errorf("no snapshot")
	}
	if ctx.DryRun {
		ctx.UI.Detail("would restore /etc/ufw/after.rules")
		return nil
	}
	return snap.RestoreFile("/etc/ufw/after.rules")
}
