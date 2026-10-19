package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The plugin packages are checked here because they name the executable's
// version and its MCP command; a release that changes either must update them.
const pluginDir = "../../plugins/nemalo"

func readJSON(t *testing.T, name string, value any) {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		t.Fatal(name, err)
	}
}

type pluginManifest struct {
	Schema      string `json:"$schema"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"author"`
	Homepage   string                     `json:"homepage"`
	Repository string                     `json:"repository"`
	License    string                     `json:"license"`
	Keywords   []string                   `json:"keywords"`
	UserConfig map[string]json.RawMessage `json:"userConfig"`
}

type mcpConfig struct {
	Schema     string `json:"$schema"`
	MCPServers map[string]struct {
		Type    string   `json:"type"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	} `json:"mcpServers"`
}

func TestPluginPackagesMatchTheExecutable(t *testing.T) {
	var claude, portable pluginManifest
	readJSON(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), &claude)
	readJSON(t, filepath.Join(pluginDir, "plugin.json"), &portable)
	for _, m := range []pluginManifest{claude, portable} {
		if m.Name != "nemalo" || m.Version != Version || m.License != "Apache-2.0" || m.Description != claude.Description {
			t.Fatal("manifest identity", m.Name, m.Version, m.License)
		}
	}
	if portable.Schema != "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json" || portable.UserConfig != nil {
		t.Fatal("Agent Plugins manifest", portable.Schema)
	}

	// Claude Code: the server reads the folder the user chose at install.
	var claudeMCP, portableMCP mcpConfig
	readJSON(t, filepath.Join(pluginDir, ".mcp.json"), &claudeMCP)
	readJSON(t, filepath.Join(pluginDir, "mcp.json"), &portableMCP)
	server := claudeMCP.MCPServers["nemalo"]
	want := []string{"mcp", "serve", "--transport", "stdio", "--scope", "${user_config.scope}"}
	if server.Command != "nemalo" || strings.Join(server.Args, " ") != strings.Join(want, " ") || claude.UserConfig["scope"] == nil {
		t.Fatal("Claude Code MCP server", server)
	}
	// Agent Plugins has no user configuration; scopes come from Nemalo's own
	// configured library and review folders.
	portableServer := portableMCP.MCPServers["nemalo"]
	if portableMCP.Schema != "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json" || portableServer.Type != "stdio" || portableServer.Command != "nemalo" || strings.Join(portableServer.Args, " ") != "mcp serve --transport stdio" {
		t.Fatal("Agent Plugins MCP server", portableMCP)
	}

	var marketplace struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Owner       struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"owner"`
		Plugins []struct {
			Name        string `json:"name"`
			Source      string `json:"source"`
			Description string `json:"description"`
		} `json:"plugins"`
	}
	readJSON(t, "../../.claude-plugin/marketplace.json", &marketplace)
	if len(marketplace.Plugins) != 1 || marketplace.Plugins[0].Name != "nemalo" || filepath.Clean(filepath.Join("../..", marketplace.Plugins[0].Source)) != filepath.Clean(pluginDir) {
		t.Fatal("marketplace entry", marketplace.Plugins)
	}
}

// The skill keeps to the Agent Skills fields so one file serves Claude Code,
// Agent Plugins hosts, and skill uploads.
func TestPluginSkillUsesPortableFrontmatter(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(pluginDir, "skills", "nemalo", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	front, body, ok := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
	if !strings.HasPrefix(text, "---\n") || !ok || strings.TrimSpace(body) == "" {
		t.Fatal("SKILL.md needs YAML frontmatter and a body")
	}
	allowed := map[string]bool{"name": true, "description": true, "license": true, "compatibility": true, "metadata": true, "allowed-tools": true}
	fields := map[string]string{}
	for _, line := range strings.Split(front, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found || !allowed[key] {
			t.Fatalf("unsupported frontmatter line %q", line)
		}
		fields[key] = strings.TrimSpace(value)
	}
	if !regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`).MatchString(fields["name"]) || fields["name"] != "nemalo" {
		t.Fatal("skill name must match its directory", fields["name"])
	}
	if d := fields["description"]; d == "" || len(d) > 1024 {
		t.Fatal("description length", len(d))
	}
	if c := fields["compatibility"]; len(c) > 500 {
		t.Fatal("compatibility length", len(c))
	}
	// Every tool the skill names exists on a server with network and write
	// tools enabled.
	server, err := mcpserver.New(app.Service{}, mcpserver.Options{Scopes: []string{t.TempDir()}, Network: true, Write: true, Version: Version})
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, st) }()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]bool{}
	for _, tool := range list.Tools {
		tools[tool.Name] = true
	}
	named := regexp.MustCompile("`((?:nemalo|library|content|catalog|file)_[a-z_]+)`").FindAllStringSubmatch(text, -1)
	if len(named) < 8 {
		t.Fatal("skill names too few tools", named)
	}
	for _, tool := range named {
		if !tools[tool[1]] {
			t.Fatal("skill names an unknown tool", tool[1])
		}
	}
}
