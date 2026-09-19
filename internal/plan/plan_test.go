package plan

import "testing"

func TestEmpty(t *testing.T) {
	if !(Plan{}).Empty() {
		t.Fatal("zero plan should be empty")
	}
	if Recommended().Empty() {
		t.Fatal("recommended should not be empty")
	}
}

func TestModuleNamesOrder(t *testing.T) {
	p := Recommended()
	p.Dokploy = true
	got := p.ModuleNames()
	want := []string{"user", "ssh", "ufw", "fail2ban", "updates", "docker", "dokploy"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v want %v", got, want)
		}
	}
}
