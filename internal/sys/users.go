package sys

import (
	"bufio"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultOperator = "deploy"

// DetectOperator picks the account to harden instead of inventing a new one:
// the user who ran sudo, then an existing sudo-capable user with SSH keys
// (cloud images ship ubuntu / debian / admin), then DefaultOperator.
func DetectOperator() string {
	if u := os.Getenv("SUDO_USER"); u != "" && u != "root" && UserExists(u) {
		return u
	}
	if me := CurrentUsername(); me != "" && me != "root" {
		return me
	}
	if users := ExistingKeyedSudoers(); len(users) > 0 {
		return users[0]
	}
	return DefaultOperator
}

// ExistingKeyedSudoers lists human accounts (uid 1000–59999) that are in the
// sudo group and already have an SSH key.
func ExistingKeyedSudoers() []string {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return nil
	}
	defer f.Close()
	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), ":")
		if len(parts) < 7 {
			continue
		}
		uid, err := strconv.Atoi(parts[2])
		if err != nil || uid < 1000 || uid >= 60000 {
			continue
		}
		name, home := parts[0], parts[5]
		if !InSudoGroup(name) {
			continue
		}
		if len(AuthorizedKeys(filepath.Join(home, ".ssh", "authorized_keys"))) == 0 {
			continue
		}
		names = append(names, name)
	}
	return names
}

func InSudoGroup(name string) bool {
	u, err := user.Lookup(name)
	if err != nil {
		return false
	}
	g, err := user.LookupGroup("sudo")
	if err != nil {
		return false
	}
	ids, err := u.GroupIds()
	if err != nil {
		return false
	}
	for _, id := range ids {
		if id == g.Gid {
			return true
		}
	}
	return false
}

// HasPassword reports whether the account has a usable login password.
func HasPassword(name string) bool {
	data, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) < 2 || parts[0] != name {
			continue
		}
		hash := parts[1]
		return hash != "" && !strings.HasPrefix(hash, "!") && !strings.HasPrefix(hash, "*")
	}
	return false
}

// AuthorizedKeys returns the keys in an authorized_keys file as
// "type blob [comment]", dropping per-key options. Cloud images prefix root's
// keys with command="echo Please login as ubuntu…", which must not be copied.
func AuthorizedKeys(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return ParseAuthorizedKeys(string(data))
}

func ParseAuthorizedKeys(data string) []string {
	var keys []string
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key := bareKey(line); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

// KeyBlob returns the base64 part used to compare keys regardless of options or comment.
func KeyBlob(line string) string {
	fields := strings.Fields(bareKey(line))
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func bareKey(line string) string {
	fields := strings.Fields(line)
	for i, f := range fields {
		if isKeyType(f) && i+1 < len(fields) {
			return strings.Join(fields[i:], " ")
		}
	}
	return ""
}

func isKeyType(s string) bool {
	return strings.HasPrefix(s, "ssh-") ||
		strings.HasPrefix(s, "ecdsa-sha2-") ||
		strings.HasPrefix(s, "sk-ssh-") ||
		strings.HasPrefix(s, "sk-ecdsa-")
}
