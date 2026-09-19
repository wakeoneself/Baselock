package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wakeoneself/Baselock/internal/backup"
	"github.com/wakeoneself/Baselock/internal/sys"
)

const (
	sshDropIn   = "/etc/ssh/sshd_config.d/99-sec.conf"
	sudoersPref = "/etc/sudoers.d/sec-"
)

type User struct{}

func (User) Name() string { return "user" }

func (User) sudoersPath(username string) string {
	return sudoersPref + username
}

func (m User) Apply(ctx Context) error {
	username := ctx.Plan.Username
	if username == "" {
		username = "deploy"
	}
	if username == "root" {
		return fmt.Errorf("refusing to use root as the operator user")
	}

	keySrc, err := m.resolveKeys(ctx)
	if err != nil {
		if ctx.DryRun {
			ctx.UI.Detail(err.Error())
			ctx.UI.Detail("would create user " + username + " once a public key is provided")
			return nil
		}
		return err
	}

	sudoers := m.sudoersPath(username)
	if ctx.Snapshot != nil {
		_ = ctx.Snapshot.SaveFile(sshDropIn)
		_ = ctx.Snapshot.SaveFile(sudoers)
		_ = ctx.Snapshot.SaveNote("user-name", []byte(username))
	}

	if ctx.DryRun {
		ctx.UI.Detail("would create user " + username + " in sudo" + dockerSuffix())
		ctx.UI.Detail("would install authorized_keys from " + keySrc)
		ctx.UI.Detail("would write " + sudoers)
		ctx.UI.Detail("would set PermitRootLogin prohibit-password (root SSH keys stay as break-glass)")
		return nil
	}

	if !sys.UserExists(username) {
		if err := sys.RunOK("adduser", "--disabled-password", "--gecos", "", username); err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		ctx.UI.Detail("created user " + username)
	} else {
		ctx.UI.Detail("user " + username + " already exists")
	}

	if err := sys.RunOK("usermod", "-aG", "sudo", username); err != nil {
		return fmt.Errorf("add sudo group: %w", err)
	}
	if sys.GroupExists("docker") {
		_ = sys.RunOK("usermod", "-aG", "docker", username)
		ctx.UI.Detail("added " + username + " to docker")
	}

	home, err := sys.HomeDir(username)
	if err != nil {
		return err
	}
	sshDir := filepath.Join(home, ".ssh")
	auth := filepath.Join(sshDir, "authorized_keys")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}
	keys, err := os.ReadFile(keySrc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(auth, keys, 0o600); err != nil {
		return err
	}
	_ = sys.RunOK("chown", "-R", username+":"+username, sshDir)
	if !sys.HasAuthorizedKeys(auth) {
		return fmt.Errorf("no usable SSH public key in %s — refusing to disable root SSH", auth)
	}

	// Key-only account (--disabled-password): there is no login password to
	// type at sudo, so NOPASSWD is the only setup that works.
	sudoLine := username + " ALL=(ALL) NOPASSWD:ALL\n"
	if err := sys.WriteFile(sudoers, []byte(sudoLine), 0o440); err != nil {
		return err
	}
	if err := sys.RunOK("visudo", "-cf", sudoers); err != nil {
		_ = os.Remove(sudoers)
		return fmt.Errorf("invalid sudoers: %w", err)
	}
	if out, err := sys.Run("sudo", "-U", username, "-l"); err != nil || strings.TrimSpace(out) == "" {
		return fmt.Errorf("sudo check failed for %s: %v", username, err)
	}

	prev, _ := os.ReadFile(sshDropIn)
	rootLogin := "prohibit-password"
	if ctx.Plan.DisableRootSSH {
		rootLogin = "no"
	}
	dropIn := mergeSSHDropIn(string(prev), map[string]string{
		"PubkeyAuthentication": "yes",
		"PermitRootLogin":      rootLogin,
	})
	if err := sys.WriteFile(sshDropIn, []byte(dropIn), 0o644); err != nil {
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
	if ctx.Plan.DisableRootSSH {
		ctx.UI.Detail("PermitRootLogin no — keep a second SSH session open")
	} else {
		ctx.UI.Detail("PermitRootLogin prohibit-password — root SSH with keys still works (no console password needed)")
	}
	ctx.UI.Detail(username + " has no password; sudo is passwordless (SSH key is the login)")
	return nil
}

func restoreSSHDropIn(prev []byte) error {
	if len(prev) == 0 {
		if sys.FileExists(sshDropIn) {
			return os.Remove(sshDropIn)
		}
		return nil
	}
	return sys.WriteFile(sshDropIn, prev, 0o644)
}

func (m User) resolveKeys(ctx Context) (string, error) {
	if ctx.Plan.SSHPubKey != "" {
		if !sys.HasAuthorizedKeys(ctx.Plan.SSHPubKey) {
			return "", fmt.Errorf("no usable public key in %s", ctx.Plan.SSHPubKey)
		}
		return ctx.Plan.SSHPubKey, nil
	}
	if ctx.Plan.CopyRootKeys || ctx.Plan.SSHPubKey == "" {
		rootKeys := "/root/.ssh/authorized_keys"
		if sys.HasAuthorizedKeys(rootKeys) {
			return rootKeys, nil
		}
	}
	return "", fmt.Errorf("no SSH public key for the operator — pass --ssh-pubkey or --copy-root-keys (refusing to lock you out)")
}

func dockerSuffix() string {
	if sys.GroupExists("docker") {
		return "+docker"
	}
	return ""
}

func (m User) Status() Check {
	if !sys.HostInfo().Debian {
		return skip("user", "not Ubuntu/Debian")
	}
	rootOff := permitRootClosed()
	operators := findOperators()
	if len(operators) == 0 {
		return fail("user", "no sudo operator user found", "run: sudo sec")
	}
	if !rootOff {
		return ok("user", "operator "+operators[0]+" · root SSH keys still work (break-glass)")
	}
	return ok("user", "operator "+operators[0]+" · root SSH disabled")
}

func permitRootClosed() bool {
	if data, err := os.ReadFile(sshDropIn); err == nil && strings.Contains(string(data), "PermitRootLogin no") && !strings.Contains(string(data), "prohibit-password") {
		return true
	}
	if data, err := os.ReadFile("/etc/ssh/sshd_config"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(strings.ToLower(line), "permitrootlogin") && strings.Contains(strings.ToLower(line), "no") {
				return true
			}
		}
	}
	return false
}

func findOperators() []string {
	matches, _ := filepath.Glob("/etc/sudoers.d/sec-*")
	var names []string
	for _, p := range matches {
		names = append(names, strings.TrimPrefix(filepath.Base(p), "sec-"))
	}
	return names
}

func (m User) Revert(ctx Context, snap *backup.Snapshot) error {
	if snap == nil {
		return fmt.Errorf("no snapshot")
	}
	if ctx.DryRun {
		ctx.UI.Detail("would restore " + sshDropIn + " and sudoers")
		return nil
	}
	if err := snap.RestoreFile(sshDropIn); err != nil {
		return err
	}
	username := ctx.Plan.Username
	if data, err := snap.Note("user-name"); err == nil && len(data) > 0 {
		username = string(data)
	}
	if username != "" {
		if err := snap.RestoreFile(m.sudoersPath(username)); err != nil {
			return err
		}
	}
	if ctx.Plan.PurgeUser && username != "" && username != "root" && sys.UserExists(username) {
		_ = sys.RunOK("deluser", "--remove-home", username)
		ctx.UI.Detail("removed user " + username)
	}
	return sys.ReloadSSH()
}
