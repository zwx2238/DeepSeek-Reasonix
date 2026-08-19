package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBrokerManagedForRootIgnoresProjectConfiguration(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")

	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(`
default_model = "managed"

[[providers]]
name = "managed"
kind = "openai"
base_url = "https://example.invalid/v1"
model = "managed-model"
api_key_env = "MANAGED_API_KEY"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("MANAGED_API_KEY=managed-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
default_model = "project"

[[providers]]
name = "project"
kind = "openai"
base_url = "https://project.invalid/v1"
model = "project-model"
api_key_env = "PROJECT_API_KEY"

[[plugins]]
name = "project-plugin"
command = "project-owned-command"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".mcp.json"), []byte(`{
  "mcpServers": {
    "project-json": {
      "command": "project-owned-command"
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadBrokerManagedForRoot(project)
	if err != nil {
		t.Fatalf("LoadBrokerManagedForRoot: %v", err)
	}
	if cfg.DefaultModel != "managed" {
		t.Fatalf("default_model = %q, want managed", cfg.DefaultModel)
	}
	if len(cfg.Providers) != 1 || cfg.Providers[0].Name != "managed" {
		t.Fatalf("providers = %+v, want only managed provider", cfg.Providers)
	}
	if cfg.Providers[0].APIKey() != "managed-secret" {
		t.Fatal("managed provider credential was not resolved from REASONIX_HOME/.env")
	}
	if len(cfg.Plugins) != 0 {
		t.Fatalf("plugins = %+v, want none", cfg.Plugins)
	}
}
