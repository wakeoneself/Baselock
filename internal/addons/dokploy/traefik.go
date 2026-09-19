package dokploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	pathDeploy    = "/api/deploy"
	pathProviders = "/api/providers"
)

// RestrictRule keeps Host(...) and requires webhook/OAuth path prefixes.
func RestrictRule(rule string) string {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return pathRule("")
	}
	if strings.Contains(rule, pathDeploy) && strings.Contains(rule, pathProviders) {
		return rule
	}
	host := extractHostRule(rule)
	return pathRule(host)
}

func pathRule(hostRule string) string {
	paths := fmt.Sprintf("(PathPrefix(`%s`) || PathPrefix(`%s`))", pathDeploy, pathProviders)
	if hostRule == "" {
		return paths
	}
	return hostRule + " && " + paths
}

func extractHostRule(rule string) string {
	// Prefer a Host(`...`) token if present.
	lower := rule
	idx := strings.Index(strings.ToLower(lower), "host(")
	if idx < 0 {
		return ""
	}
	rest := rule[idx:]
	// Host(`x`) or Host("x") or Host(x)
	depth := 0
	for i, r := range rest {
		if r == '(' {
			depth++
		}
		if r == ')' {
			depth--
			if depth == 0 {
				return strings.TrimSpace(rest[:i+1])
			}
		}
	}
	return ""
}

func ExtractHostFromRule(rule string) string {
	hostRule := extractHostRule(rule)
	if hostRule == "" {
		return ""
	}
	// Host(`example.com`)
	start := strings.IndexAny(hostRule, "`\"'")
	if start < 0 {
		return ""
	}
	q := hostRule[start]
	end := strings.IndexByte(hostRule[start+1:], q)
	if end < 0 {
		return ""
	}
	return hostRule[start+1 : start+1+end]
}

type TraefikFile struct {
	Path string
	Raw  []byte
	Doc  map[string]any
}

func LoadTraefik(path string) (*TraefikFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &TraefikFile{Path: path, Raw: raw, Doc: doc}, nil
}

func (t *TraefikFile) RouterRules() []string {
	var rules []string
	http, _ := t.Doc["http"].(map[string]any)
	if http == nil {
		return nil
	}
	routers, _ := http["routers"].(map[string]any)
	for _, v := range routers {
		rm, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if rule, ok := rm["rule"].(string); ok {
			rules = append(rules, rule)
		}
	}
	return rules
}

func (t *TraefikFile) LooksLikeDokployPanel() bool {
	http, _ := t.Doc["http"].(map[string]any)
	if http == nil {
		return false
	}
	services, _ := http["services"].(map[string]any)
	for name, v := range services {
		n := strings.ToLower(name)
		if strings.Contains(n, "dokploy") {
			return true
		}
		rm, _ := v.(map[string]any)
		if pointsAtDokploy(rm) {
			return true
		}
	}
	routers, _ := http["routers"].(map[string]any)
	for name := range routers {
		if strings.Contains(strings.ToLower(name), "dokploy") {
			return true
		}
	}
	return false
}

func pointsAtDokploy(svc map[string]any) bool {
	lb, _ := svc["loadBalancer"].(map[string]any)
	if lb == nil {
		return false
	}
	servers, _ := lb["servers"].([]any)
	for _, s := range servers {
		sm, _ := s.(map[string]any)
		url, _ := sm["url"].(string)
		if strings.Contains(strings.ToLower(url), "dokploy") || strings.HasSuffix(url, ":3000") {
			return true
		}
	}
	return false
}

func (t *TraefikFile) RestrictPanelRoutes() bool {
	http, _ := t.Doc["http"].(map[string]any)
	if http == nil {
		return false
	}
	routers, _ := http["routers"].(map[string]any)
	if routers == nil {
		return false
	}
	changed := false
	for name, v := range routers {
		rm, ok := v.(map[string]any)
		if !ok {
			continue
		}
		svc, _ := rm["service"].(string)
		if !strings.Contains(strings.ToLower(name), "dokploy") &&
			!strings.Contains(strings.ToLower(svc), "dokploy") {
			continue
		}
		rule, _ := rm["rule"].(string)
		next := RestrictRule(rule)
		if next != rule {
			rm["rule"] = next
			changed = true
		}
	}
	return changed
}

func (t *TraefikFile) DetectPublicHost() string {
	for _, rule := range t.RouterRules() {
		if h := ExtractHostFromRule(rule); h != "" && h != "localhost" && h != "127.0.0.1" {
			return h
		}
	}
	return ""
}

func (t *TraefikFile) Write() error {
	raw, err := yaml.Marshal(t.Doc)
	if err != nil {
		return err
	}
	return os.WriteFile(t.Path, raw, 0o644)
}

func FindPanelFiles(root string) ([]string, error) {
	if root == "" {
		root = "/etc/dokploy/traefik"
	}
	var found []string
	candidates := []string{
		filepath.Join(root, "dynamic", "dokploy.yml"),
		filepath.Join(root, "dynamic", "dokploy.yaml"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			found = append(found, c)
		}
	}
	dyn := filepath.Join(root, "dynamic")
	entries, err := os.ReadDir(dyn)
	if err != nil {
		if len(found) > 0 {
			return found, nil
		}
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range found {
		seen[f] = true
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		path := filepath.Join(dyn, e.Name())
		if seen[path] {
			continue
		}
		tf, err := LoadTraefik(path)
		if err != nil {
			continue
		}
		if tf.LooksLikeDokployPanel() {
			found = append(found, path)
		}
	}
	return found, nil
}

func WebhookOnlyDoc(host string) map[string]any {
	rule := pathRule("")
	if host != "" {
		rule = pathRule(fmt.Sprintf("Host(`%s`)", host))
	}
	return map[string]any{
		"http": map[string]any{
			"routers": map[string]any{
				"dokploy-webhooks": map[string]any{
					"rule":        rule,
					"service":     "dokploy-service-app",
					"entryPoints": []any{"web", "websecure"},
				},
			},
			"services": map[string]any{
				"dokploy-service-app": map[string]any{
					"loadBalancer": map[string]any{
						"servers": []any{
							map[string]any{"url": "http://dokploy:3000", "passHostHeader": true},
						},
					},
				},
			},
		},
	}
}

func WriteWebhookOnly(path, host string) error {
	doc := WebhookOnlyDoc(host)
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
