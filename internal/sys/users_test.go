package sys

import "testing"

func TestParseAuthorizedKeysStripsCloudRootOptions(t *testing.T) {
	data := `# comment
no-port-forwarding,no-agent-forwarding,no-X11-forwarding,command="echo 'Please login as the user \"ubuntu\" rather than the user \"root\".';echo;sleep 10;exit 142" ssh-ed25519 AAAAC3Nza me@laptop
ssh-rsa AAAAB3Nza other
garbage line
`
	keys := ParseAuthorizedKeys(data)
	if len(keys) != 2 {
		t.Fatalf("want 2 keys, got %d: %v", len(keys), keys)
	}
	if keys[0] != "ssh-ed25519 AAAAC3Nza me@laptop" {
		t.Fatalf("options not stripped: %q", keys[0])
	}
	if KeyBlob(keys[1]) != "AAAAB3Nza" {
		t.Fatalf("blob: %q", KeyBlob(keys[1]))
	}
}
