package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/pluginpkg"
)

func TestLoadMergesInstalledPluginSkillRootsAndMCP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "superpowers")
	writeConfigTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "superpowers",
  "version": "1.0.0",
  "skills": "skills",
  "mcpServers": {
    "helper": { "command": "bin/helper" }
  }
}`)
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{
		Name:         "superpowers",
		Root:         "plugins/superpowers",
		Version:      "1.0.0",
		ManifestKind: "reasonix",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Skills.Paths) == 0 || cfg.Skills.Paths[len(cfg.Skills.Paths)-1] != filepath.Join(root, "skills") {
		t.Fatalf("skills paths = %#v", cfg.Skills.Paths)
	}
	owners := cfg.PluginPackageSkillOwners()[CanonicalSkillPath(filepath.Join(root, "skills"))]
	if len(owners) != 1 || owners[0] != "superpowers" {
		t.Fatalf("plugin skill owners = %#v, want superpowers", owners)
	}
	var found bool
	for _, p := range cfg.Plugins {
		if p.Name == "helper" {
			found = true
			if p.Command != filepath.Join(root, "bin", "helper") {
				t.Fatalf("plugin command = %q", p.Command)
			}
			if p.Env["REASONIX_PLUGIN_NAME"] != "superpowers" {
				t.Fatalf("plugin env = %#v", p.Env)
			}
		}
	}
	if !found {
		t.Fatalf("plugin MCP server missing: %#v", cfg.Plugins)
	}
	if owner, ok := cfg.PluginPackageOwner("helper"); !ok || owner != "superpowers" {
		t.Fatalf("plugin MCP owner = %q, %v; want superpowers, true", owner, ok)
	}
}

func TestClaudePackageMCPExpandsRootAndDoesNotAutoStart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "claude-mcp")
	writeConfigTestFile(t, filepath.Join(root, pluginpkg.ClaudeManifest), `{"name":"claude-mcp"}`)
	writeConfigTestFile(t, filepath.Join(root, ".mcp.json"), `{
  "mcpServers": {
    "Local Search": {
      "command": "${CLAUDE_PLUGIN_ROOT}/bin/server",
      "args": ["--root", "${CLAUDE_PLUGIN_ROOT}/data", "--workspace", "${CLAUDE_PROJECT_DIR}"],
      "env": {"DATA_DIR": "${CLAUDE_PLUGIN_ROOT}/data"}
    }
  }
}`)
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "claude-mcp", Root: "plugins/claude-mcp", ManifestKind: "claude", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	cfg, err := LoadForRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 1 {
		t.Fatalf("plugins = %#v", cfg.Plugins)
	}
	got := cfg.Plugins[0]
	if got.Command != filepath.Join(root, "bin", "server") || got.Args[1] != filepath.Join(root, "data") || got.Env["DATA_DIR"] != filepath.Join(root, "data") {
		t.Fatalf("Claude root was not expanded: %#v", got)
	}
	if got.Env["CLAUDE_PLUGIN_ROOT"] != root {
		t.Fatalf("CLAUDE_PLUGIN_ROOT = %q", got.Env["CLAUDE_PLUGIN_ROOT"])
	}
	if got.Args[3] != workspace || got.Env["CLAUDE_PROJECT_DIR"] != workspace {
		t.Fatalf("workspace expansion = %#v", got)
	}
	if got.ShouldAutoStart() {
		t.Fatal("imported Claude MCP must require an explicit connection")
	}
	if len(cfg.AutoStartPlugins()) != 0 {
		t.Fatalf("auto-start plugins = %#v", cfg.AutoStartPlugins())
	}
}

func TestClaudePackageMCPDeduplicatesSameConnectionAcrossPackages(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	for i, name := range []string{"legal-one", "legal-two"} {
		root := filepath.Join(home, "plugins", name)
		writeConfigTestFile(t, filepath.Join(root, pluginpkg.ClaudeManifest), `{"name":"`+name+`"}`)
		writeConfigTestFile(t, filepath.Join(root, ".mcp.json"), `{
  "mcpServers":{"飞书":{"type":"http","url":"https://open.feishu.cn/mcp","description":"package `+string(rune('A'+i))+` description"}}
}`)
		if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: name, Root: "plugins/" + name, ManifestKind: "claude", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := LoadForRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].URL != "https://open.feishu.cn/mcp" {
		t.Fatalf("deduplicated plugins = %#v", cfg.Plugins)
	}
}

func writeConfigTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCommandDirsIncludePluginPackageCommands pins the plugin-commands wiring:
// an enabled plugin package's command roots join command discovery at the
// before user/project entries, retaining package ownership, while a disabled
// package contributes nothing.
func TestCommandDirsIncludePluginPackageCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "pwf")
	writeConfigTestFile(t, filepath.Join(root, pluginpkg.ClaudeManifest), `{"name": "pwf"}`)
	writeConfigTestFile(t, filepath.Join(root, "skills", "planner", "SKILL.md"), "---\ndescription: p\n---\nbody")
	writeConfigTestFile(t, filepath.Join(root, "commands", "plan.md"), "---\ndescription: plan\n---\nPlan: $ARGUMENTS")
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{
		Name:         "pwf",
		Root:         "plugins/pwf",
		ManifestKind: "claude",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}

	dirs := CommandDirsForRoot(t.TempDir())
	want := filepath.Join(root, "commands")
	if len(dirs) == 0 || dirs[0] != want {
		t.Fatalf("CommandDirsForRoot = %#v, want plugin commands dir first (lowest priority): %s", dirs, want)
	}
	roots := CommandRootsForRoot(t.TempDir())
	if len(roots) == 0 || roots[0].Path != want || roots[0].Plugin != "pwf" {
		t.Fatalf("CommandRootsForRoot = %#v, want plugin ownership on first root", roots)
	}

	if err := pluginpkg.SetEnabled(home, "pwf", false); err != nil {
		t.Fatal(err)
	}
	for _, dir := range CommandDirsForRoot(t.TempDir()) {
		if dir == want {
			t.Fatalf("disabled plugin's commands dir must not join discovery: %#v", dir)
		}
	}
}

// TestCommandDirsWithoutPluginState keeps the no-plugin fast path intact.
func TestCommandDirsWithoutPluginState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if dirs := CommandDirsForRoot(t.TempDir()); len(dirs) == 0 {
		t.Fatal("CommandDirsForRoot must still return the conventional dirs")
	}
}

// TestCommandRootsForRootSkipsRealHomeWhenIsolated pins the Agent Home
// isolation contract for custom slash commands: once REASONIX_HOME points at
// a sandbox home, CommandRootsForRoot must not enumerate ~/.claude/commands,
// ~/.agents/commands, ~/.agent/commands or ~/.reasonix/commands from the real
// user HOME. The plugin roots, the Reasonix-home commands dir, and the
// project convention subdirs under the workspace root must still load so the
// isolated runtime keeps its own authoring surface.
func TestCommandRootsForRootSkipsRealHomeWhenIsolated(t *testing.T) {
	isolated := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("REASONIX_HOME", isolated)
	// Forge a real HOME that contains a Claude-style commands dir; the isolated
	// runtime must not pick it up.
	realHome := t.TempDir()
	realClaude := filepath.Join(realHome, ".claude", "commands")
	if err := os.MkdirAll(realClaude, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realClaude, "sneak.md"), []byte("# leaked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", realHome)
	if runtimeGOOS == "windows" {
		t.Setenv("USERPROFILE", realHome)
	}

	roots := CommandRootsForRoot(workspace)
	for _, r := range roots {
		if strings.HasPrefix(r.Path, realClaude) {
			t.Fatalf("CommandRootsForRoot leaked real HOME Claude commands dir %q under isolated REASONIX_HOME %q: %#v", r.Path, isolated, roots)
		}
		if strings.HasPrefix(r.Path, filepath.Join(realHome, ".agents", "commands")) ||
			strings.HasPrefix(r.Path, filepath.Join(realHome, ".agent", "commands")) ||
			strings.HasPrefix(r.Path, filepath.Join(realHome, ".reasonix", "commands")) {
			t.Fatalf("CommandRootsForRoot leaked real HOME convention dir %q under isolated REASONIX_HOME: %#v", r.Path, roots)
		}
	}

	// The isolated Reasonix home commands dir must still be present.
	wantIsolatedCommands := filepath.Join(isolated, "commands")
	var sawIsolated bool
	for _, r := range roots {
		if samePath(r.Path, wantIsolatedCommands) {
			sawIsolated = true
			break
		}
	}
	if !sawIsolated {
		t.Fatalf("CommandRootsForRoot under isolated REASONIX_HOME must still include %q, got %#v", wantIsolatedCommands, roots)
	}

	// Project convention subdirs under the workspace must still be discovered.
	wantProjectClaude := filepath.Join(workspace, ".claude", "commands")
	var sawProject bool
	for _, r := range roots {
		if samePath(r.Path, wantProjectClaude) {
			sawProject = true
			break
		}
	}
	if !sawProject {
		t.Fatalf("CommandRootsForRoot under isolated REASONIX_HOME must still include project convention dir %q, got %#v", wantProjectClaude, roots)
	}
}

// TestCommandRootsForRootIncludesRealHomeClaudeByDefault pins the pre-isolation
// behaviour: without REASONIX_HOME the loader still walks the real user HOME
// convention subdirs (notably ~/.claude/commands) so existing installs keep
// working. This is the counterpart to the isolated test above and ensures the
// isolation guard does not regress the default path.
func TestCommandRootsForRootIncludesRealHomeClaudeByDefault(t *testing.T) {
	t.Setenv("REASONIX_HOME", "")
	realHome := t.TempDir()
	realClaude := filepath.Join(realHome, ".claude", "commands")
	if err := os.MkdirAll(realClaude, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realClaude, "hello.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", realHome)
	if runtimeGOOS == "windows" {
		t.Setenv("USERPROFILE", realHome)
	}

	roots := CommandRootsForRoot(t.TempDir())
	var sawClaude bool
	for _, r := range roots {
		if samePath(r.Path, realClaude) {
			sawClaude = true
			break
		}
	}
	if !sawClaude {
		t.Fatalf("CommandRootsForRoot without REASONIX_HOME must include real HOME %q, got %#v", realClaude, roots)
	}
}
