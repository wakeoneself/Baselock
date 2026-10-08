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
		username = sys.DetectOperator()
	}
	if username == "root" {
		return fmt.Errorf("refusing to use root as the operator user")
	}

	exists := sys.UserExists(username)
	home := "/home/" + username
	if exists {
		if h, err := sys.HomeDir(username); err == nil {
			home = h
		}
	}
	sshDir := filepath.Join(home, ".ssh")
	auth := filepath.Join(sshDir, "authorized_keys")

	kept, add, sources, err := m.collectKeys(ctx, username, auth)
	if err != nil {
		if ctx.DryRun {
			ctx.UI.Detail(err.Error())
			return nil
		}
		return err
	}

	sudoers := m.sudoersPath(username)
	if ctx.Snapshot != nil {
		_ = ctx.Snapshot.SaveFile(sshDropIn)
		_ = ctx.Snapshot.SaveFile(sudoers)
		_ = ctx.Snapshot.SaveFile(auth)
		_ = ctx.Snapshot.SaveNote("user-name", []byte(username))
		_ = ctx.Snapshot.SaveNote("user-auth", []byte(auth))
		if !exists {
			_ = ctx.Snapshot.SaveNote("user-created", []byte("1"))
		}
	}

	if ctx.DryRun {
		if exists {
			ctx.UI.Detail(fmt.Sprintf("would reuse existing user %s (keeps its %d SSH key(s))", username, kept))
		} else {
			ctx.UI.Detail("would create user " + username + " in sudo" + dockerSuffix())
		}
		if len(add) > 0 {
			ctx.UI.Detail(fmt.Sprintf("would add %d key(s) from %s", len(add), strings.Join(sources, ", ")))
		}
		ctx.UI.Detail("would write " + sudoers)
		ctx.UI.Detail("would set PermitRootLogin prohibit-password (root SSH keys stay as break-glass)")
		return nil
	}

	if !exists {
		if err := sys.RunOK("adduser", "--disabled-password", "--gecos", "", username); err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		ctx.UI.Detail("created user " + username)
	} else {
		ctx.UI.Detail(fmt.Sprintf("reusing existing user %s — its %d SSH key(s) stay untouched", username, kept))
	}

	if err := sys.RunOK("usermod", "-aG", "sudo", username); err != nil {
		return fmt.Errorf("add sudo group: %w", err)
	}
	if sys.GroupExists("docker") {
		_ = sys.RunOK("usermod", "-aG", "docker", username)
		ctx.UI.Detail("added " + username + " to docker")
	}

	if len(add) > 0 {
		if err := appendKeys(sshDir, auth, add); err != nil {
			return err
		}
		_ = sys.RunOK("chown", "-R", username+":"+username, sshDir)
		ctx.UI.Detail(fmt.Sprintf("added %d SSH key(s) from %s", len(add), strings.Join(sources, ", ")))
	}
	if !sys.HasAuthorizedKeys(auth) {
		return fmt.Errorf("no usable SSH public key in %s — refusing to continue", auth)
	}

	// Key-only accounts have no password to type at sudo, so they need
	// NOPASSWD. Accounts that already have a password keep asking for it.
	sudoLine := username + " ALL=(ALL) NOPASSWD:ALL\n"
	if exists && sys.HasPassword(username) {
		sudoLine = username + " ALL=(ALL:ALL) ALL\n"
		ctx.UI.Detail(username + " has a password — sudo keeps asking for it")
	}
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
	if !sys.HasPassword(username) {
		ctx.UI.Detail(username + " has no password; sudo is passwordless (SSH key is the login)")
	}
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

// collectKeys never replaces the operator's keys: it counts what is already in
// auth and returns only keys from other sources that are missing there.
func (m User) collectKeys(ctx Context, username, auth string) (kept int, add, sources []string, err error) {
	have := map[string]bool{}
	for _, k := range sys.AuthorizedKeys(auth) {
		have[sys.KeyBlob(k)] = true
	}
	kept = len(have)

	type source struct{ label, path string }
	var srcs []source
	if ctx.Plan.SSHPubKey != "" {
		if !sys.HasAuthorizedKeys(ctx.Plan.SSHPubKey) {
			return 0, nil, nil, fmt.Errorf("no usable public key in %s", ctx.Plan.SSHPubKey)
		}
		srcs = append(srcs, source{"--ssh-pubkey", ctx.Plan.SSHPubKey})
	}
	if su := os.Getenv("SUDO_USER"); su != "" && su != "root" && su != username {
		if h, err := sys.HomeDir(su); err == nil {
			srcs = append(srcs, source{su, filepath.Join(h, ".ssh", "authorized_keys")})
		}
	}
	if ctx.Plan.CopyRootKeys {
		srcs = append(srcs, source{"root", "/root/.ssh/authorized_keys"})
	}

	for _, s := range srcs {
		added := false
		for _, k := range sys.AuthorizedKeys(s.path) {
			b := sys.KeyBlob(k)
			if have[b] {
				continue
			}
			have[b] = true
			add = append(add, k)
			added = true
		}
		if added {
			sources = append(sources, s.label)
		}
	}
	if len(have) == 0 {
		return 0, nil, nil, fmt.Errorf("no SSH key found for %s or root — add one first (ssh-copy-id %s@%s) or pass --ssh-pubkey FILE (refusing to lock you out)", username, username, sys.PublicIP())
	}
	return kept, add, sources, nil
}

func appendKeys(sshDir, auth string, keys []string) error {
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}
	prev, _ := os.ReadFile(auth)
	out := string(prev)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += strings.Join(keys, "\n") + "\n"
	return os.WriteFile(auth, []byte(out), 0o600)
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
		if me := sys.CurrentUsername(); me != "" && me != "root" {
			return ok("user", "logged in as "+me+" · run sudo sec status for the full check")
		}
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
	if data, err := snap.Note("user-auth"); err == nil && len(data) > 0 {
		if err := snap.RestoreFile(string(data)); err != nil {
			return err
		}
	}
	_, createdErr := snap.Note("user-created")
	created := createdErr == nil
	if ctx.Plan.PurgeUser && username != "" && username != "root" && sys.UserExists(username) {
		if created {
			_ = sys.RunOK("deluser", "--remove-home", username)
			ctx.UI.Detail("removed user " + username)
		} else {
			ctx.UI.Detail("kept " + username + " — it existed before sec")
		}
	}
	return sys.ReloadSSH()
}
