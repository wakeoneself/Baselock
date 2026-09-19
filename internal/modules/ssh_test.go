package modules

import (
	"strings"
	"testing"
)

func TestMergeSSHDropInKeepsKnownKeysOnly(t *testing.T) {
	got := mergeSSHDropIn("", map[string]string{
		"PubkeyAuthentication":   "yes",
		"PasswordAuthentication": "no",
	})
	if strings.Contains(got, "KbdInteractiveAuthentication") {
		t.Fatal("old OpenSSH rejects KbdInteractiveAuthentication — do not write it")
	}
}

func TestMergeSSHDropIn(t *testing.T) {
	got := mergeSSHDropIn("PermitRootLogin no\n", map[string]string{
		"PasswordAuthentication": "no",
		"PermitRootLogin":        "no",
	})
	if !strings.Contains(got, "PermitRootLogin no") {
		t.Fatalf("lost PermitRootLogin: %s", got)
	}
	if strings.Count(got, "PermitRootLogin") != 1 {
		t.Fatalf("duplicated: %s", got)
	}
	if !strings.Contains(got, "PasswordAuthentication no") {
		t.Fatalf("missing password line: %s", got)
	}
}
