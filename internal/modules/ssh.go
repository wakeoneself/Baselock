package modules

import (
	"fmt"
	"os"
	"strings"

	"github.com/wakeoneself/Baselock/internal/backup"
	"github.com/wakeoneself/Baselock/internal/sys"
)

type SSH struct{}

func (SSH) Name() string { return "ssh" }

// SetPermitRootLogin writes PermitRootLogin and reloads sshd safely.
// Use "prohibit-password" to allow root SSH with keys, or "no" to refuse root SSH.
func SetPermitRootLogin(ctx Context, value string) error {
	if value != "no" && value != "prohibit-password" && value != "yes" {
		return fmt.Errorf("PermitRootLogin must be no, prohibit-password, or yes")
	}
	if ctx.Snapshot != nil {
		_ = ctx.Snapshot.SaveFile(sshDropIn)
	}
	if ctx.DryRun {
		ctx.UI.Detail("would set PermitRootLogin " + value + " in " + sshDropIn)
		return nil
	}
	prev, _ := os.ReadFile(sshDropIn)
	cfg := mergeSSHDropIn(string(prev), map[string]string{
		"PubkeyAuthentication": "yes",
		"PermitRootLogin":      value,
	})
	if err := sys.WriteFile(sshDropIn, []byte(cfg), 0o644); err != nil {
		return err
	}
	if err := sys.TestSSHD(); err != nil {
		_ = restoreSSHDropIn(prev)
		return fmt.Errorf("sshd rejected config, restored previous drop-in: %w", err)
	}
	if err := sys.ReloadSSH(); err != nil {
		_ = restoreSSHDropIn(prev)
		_ = sys.ReloadSSH()
		return fmt.Errorf("reload sshd: %w", err)
	}
	ctx.UI.Detail("PermitRootLogin " + value)
	return nil
}

func (SSH) Apply(ctx Context) error {
	if ctx.Snapshot != nil {
		_ = ctx.Snapshot.SaveFile(sshDropIn)
	}

	username := ctx.Plan.Username
	if username == "" {
		username = "deploy"
	}
	auth := "/home/" + username + "/.ssh/authorized_keys"
	if home, err := sys.HomeDir(username); err == nil {
		auth = home + "/.ssh/authorized_keys"
	}
	rootKeys := "/root/.ssh/authorized_keys"
	if !sys.HasAuthorizedKeys(auth) && !sys.HasAuthorizedKeys(rootKeys) {
		if ctx.DryRun {
			ctx.UI.Detail("no authorized_keys yet — would refuse to disable password SSH")
			return nil
		}
		return fmt.Errorf("no authorized_keys found — refusing to disable password SSH")
	}

	if ctx.DryRun {
		ctx.UI.Detail("would set PasswordAuthentication no in " + sshDropIn)
		return nil
	}

	prev, _ := os.ReadFile(sshDropIn)
	cfg := mergeSSHDropIn(string(prev), map[string]string{
		"PubkeyAuthentication":   "yes",
		"PasswordAuthentication": "no",
	})
	if err := sys.WriteFile(sshDropIn, []byte(cfg), 0o644); err != nil {
		return err
	}
	if err := sys.TestSSHD(); err != nil {
		_ = restoreSSHDropIn(prev)
		return fmt.Errorf("sshd rejected config, restored previous drop-in: %w", err)
	}
	if err := sys.ReloadSSH(); err != nil {
		_ = restoreSSHDropIn(prev)
		_ = sys.ReloadSSH()
		return fmt.Errorf("reload sshd: %w", err)
	}
	ctx.UI.Detail("PasswordAuthentication no (root SSH with keys still works unless you pass --disable-root-ssh)")
	return nil
}

func mergeSSHDropIn(existing string, kv map[string]string) string {
	seen := map[string]bool{}
	var lines []string
	for _, line := range strings.Split(existing, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		replaced := false
		for k, v := range kv {
			fields := strings.Fields(trim)
			if len(fields) >= 1 && strings.EqualFold(fields[0], k) {
				lines = append(lines, k+" "+v)
				seen[k] = true
				replaced = true
				break
			}
		}
		if !replaced {
			lines = append(lines, trim)
		}
	}
	for k, v := range kv {
		if !seen[k] {
			lines = append(lines, k+" "+v)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func (SSH) Status() Check {
	if !sys.HostInfo().Debian {
		return skip("ssh", "not Ubuntu/Debian")
	}
	if !sys.FileExists("/etc/ssh/sshd_config") {
		return skip("ssh", "sshd_config not found")
	}
	if passwordAuthEnabled() {
		return fail("ssh", "password authentication is enabled", "run: sudo sec --ssh")
	}
	return ok("ssh", "key-based SSH only")
}

func passwordAuthEnabled() bool {
	files := []string{sshDropIn, "/etc/ssh/sshd_config"}
	last := true
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 && strings.EqualFold(fields[0], "PasswordAuthentication") {
				last = strings.EqualFold(fields[1], "yes")
			}
		}
	}
	return last
}

func (SSH) Revert(ctx Context, snap *backup.Snapshot) error {
	if snap == nil {
		return fmt.Errorf("no snapshot")
	}
	if ctx.DryRun {
		ctx.UI.Detail("would restore " + sshDropIn)
		return nil
	}
	if err := snap.RestoreFile(sshDropIn); err != nil {
		return err
	}
	return sys.ReloadSSH()
}
