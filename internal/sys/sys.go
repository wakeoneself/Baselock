package sys

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

type Info struct {
	Hostname string
	OSName   string
	ID       string
	Like     string
	Debian   bool
}

func HostInfo() Info {
	host, _ := os.Hostname()
	info := Info{Hostname: host, OSName: runtime.GOOS}
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return info
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	kv := map[string]string{}
	for sc.Scan() {
		line := sc.Text()
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		kv[k] = strings.Trim(v, `"`)
	}
	info.ID = strings.ToLower(kv["ID"])
	info.Like = strings.ToLower(kv["ID_LIKE"])
	if pretty := kv["PRETTY_NAME"]; pretty != "" {
		info.OSName = pretty
	}
	info.Debian = info.ID == "ubuntu" || info.ID == "debian" ||
		strings.Contains(info.Like, "debian") || strings.Contains(info.Like, "ubuntu")
	return info
}

func IsRoot() bool {
	return os.Geteuid() == 0
}

func RequireRoot() error {
	if !IsRoot() {
		return fmt.Errorf("sec must run as root (sudo sec)")
	}
	return nil
}

func RequireDebian() error {
	h := HostInfo()
	if !h.Debian {
		return fmt.Errorf("sec supports Ubuntu/Debian only (detected %s)", h.OSName)
	}
	return nil
}

func CommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = out
		}
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return out, nil
}

func RunOK(name string, args ...string) error {
	_, err := Run(name, args...)
	return err
}

func WriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".sec.tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func UserExists(name string) bool {
	_, err := user.Lookup(name)
	return err == nil
}

func HomeDir(username string) (string, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

func GroupExists(name string) bool {
	_, err := user.LookupGroup(name)
	return err == nil
}

func SSHConfigDropInSupported() bool {
	return FileExists("/etc/ssh/sshd_config.d")
}

func sshdBinary() string {
	if FileExists("/usr/sbin/sshd") {
		return "/usr/sbin/sshd"
	}
	if path, err := exec.LookPath("sshd"); err == nil {
		return path
	}
	return "/usr/sbin/sshd"
}

func TestSSHD() error {
	return RunOK(sshdBinary(), "-t")
}

func ReloadSSH() error {
	if err := TestSSHD(); err != nil {
		return fmt.Errorf("sshd config invalid (refusing to reload): %w", err)
	}
	if CommandExists("systemctl") {
		if err := RunOK("systemctl", "reload", "ssh"); err == nil {
			return EnsureSSHListening()
		}
		if err := RunOK("systemctl", "reload", "sshd"); err == nil {
			return EnsureSSHListening()
		}
		// reload of a dead unit does nothing useful — start it
		_ = RunOK("systemctl", "start", "ssh")
		_ = RunOK("systemctl", "enable", "ssh")
		return EnsureSSHListening()
	}
	_ = RunOK("service", "ssh", "reload")
	return EnsureSSHListening()
}

func EnsureSSHListening() error {
	if CommandExists("systemctl") {
		out, err := Run("systemctl", "is-active", "ssh")
		if err != nil || strings.TrimSpace(out) != "active" {
			_ = RunOK("systemctl", "start", "ssh")
			out, err = Run("systemctl", "is-active", "sshd")
			if (err != nil || strings.TrimSpace(out) != "active") && FileExists("/lib/systemd/system/ssh.service") {
				if err := RunOK("systemctl", "start", "ssh"); err != nil {
					return fmt.Errorf("sshd is not active: %w", err)
				}
			}
		}
	}
	port := DetectSSHPort()
	if CommandExists("ss") {
		out, err := Run("ss", "-lnt")
		if err == nil && (strings.Contains(out, ":"+port) || strings.Contains(out, ":"+port+" ")) {
			return nil
		}
	}
	// last resort: is-active ssh
	if CommandExists("systemctl") {
		out, err := Run("systemctl", "is-active", "ssh")
		if err == nil && strings.TrimSpace(out) == "active" {
			return nil
		}
	}
	return fmt.Errorf("sshd does not appear to be listening on port %s", port)
}

func DetectSSHPort() string {
	data, err := os.ReadFile("/etc/ssh/sshd_config")
	if err != nil {
		return "22"
	}
	port := "22"
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "Port") {
			port = fields[1]
		}
	}
	return port
}

func HasAuthorizedKeys(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "ssh-") || strings.HasPrefix(line, "ecdsa-") {
			return true
		}
	}
	return false
}

func DockerInstalled() bool {
	return CommandExists("docker")
}

func DockerSwarmActive() bool {
	if !DockerInstalled() {
		return false
	}
	out, err := Run("docker", "info", "--format", "{{.Swarm.LocalNodeState}}")
	if err != nil {
		return false
	}
	state := strings.ToLower(strings.TrimSpace(out))
	return state == "active" || state == "locked"
}

func PublicIP() string {
	if out, err := Run("hostname", "-I"); err == nil {
		fields := strings.Fields(out)
		if len(fields) > 0 {
			return fields[0]
		}
	}
	return "YOUR-SERVER"
}
