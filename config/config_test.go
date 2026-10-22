package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultYAMLMatchesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := GenerateDefaultConfig(path, false); err != nil {
		t.Fatal(err)
	}
	if err := GenerateDefaultConfig(path, false); err == nil {
		t.Fatal("overwrote existing config without --force")
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
	d := Default()
	if cfg.Server.TCP.Port != d.Server.TCP.Port || cfg.Server.RESP.Port != d.Server.RESP.Port ||
		cfg.Server.REST.Port != d.Server.REST.Port || cfg.Cache.CleanupInterval != d.Cache.CleanupInterval ||
		cfg.Features.LuaTimeout != d.Features.LuaTimeout || !cfg.Server.RESP.IsEnabled() {
		t.Fatalf("default.yaml drifted from Default(): %+v", cfg.Server)
	}
}

func TestRepoConfigsAreValid(t *testing.T) {
	for _, name := range []string{"config.yaml", "config-dev.yaml", "config-prod.yaml"} {
		path := filepath.Join("..", name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		t.Setenv("BOLTCACHE_TOKEN", "test-token")
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDockerConfig(t *testing.T) {
	path := filepath.Join("..", "deploy", "docker", "config.yaml")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("docker config with defaults: %v", err)
	}
	if cfg.Security.Auth.Enabled || cfg.Persistence.File != "/data/boltcache.json" {
		t.Fatalf("unexpected defaults: %+v", cfg.Security.Auth)
	}

	t.Setenv("BOLTCACHE_AUTH", "true")
	if cfg, _ = LoadConfig(path); cfg.Validate() == nil {
		t.Fatal("auth enabled without a token should be invalid")
	}
	t.Setenv("BOLTCACHE_TOKEN", "s3cret")
	t.Setenv("BOLTCACHE_MAX_MEMORY", "256MB")
	cfg, _ = LoadConfig(path)
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if !cfg.Security.Auth.Enabled || cfg.AuthTokens()[0] != "s3cret" || cfg.Cache.MaxMemory != "256MB" {
		t.Fatalf("env not applied: %+v %+v", cfg.Security.Auth, cfg.Cache)
	}
}

func TestExpandEnv(t *testing.T) {
	t.Setenv("BC_TOKEN", "abc")
	t.Setenv("BC_EMPTY", "")
	got := ExpandEnv("a=${BC_TOKEN} b=${BC_MISSING:-def} c=${BC_EMPTY:-fallback} d=$BC_TOKEN e=${BC_MISSING}")
	want := "a=abc b=def c=fallback d=$BC_TOKEN e="
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParseMemorySize(t *testing.T) {
	cases := map[string]int64{
		"":      0,
		"0":     0,
		"1":     1,
		"100":   100,
		"1KB":   1024,
		"64kb":  64 << 10,
		"512MB": 512 << 20,
		"1GB":   1 << 30,
		"1.5G":  3 << 29,
		"2GiB":  2 << 30,
		" 4 MB": 4 << 20,
	}
	for in, want := range cases {
		got, err := ParseMemorySize(in)
		if err != nil || got != want {
			t.Errorf("ParseMemorySize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"abc", "1XB", "-1MB", "MB"} {
		if _, err := ParseMemorySize(bad); err == nil {
			t.Errorf("ParseMemorySize(%q) succeeded", bad)
		}
	}
}

func TestValidateReportsAllErrors(t *testing.T) {
	cfg := Default()
	cfg.Server.Mode = "udp"
	cfg.Cache.EvictionPolicy = "fifo"
	cfg.Security.Auth.Enabled = true
	cfg.Cache.MaxMemory = "lots"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"server.mode", "eviction_policy", "tokens", "max_memory"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't mention %s", err, want)
		}
	}
}

func TestPartialFileKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(path, []byte("cache:\n  max_keys: 5\n"), 0o644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cache.MaxKeys != 5 || cfg.Cache.CleanupInterval != time.Minute || cfg.Server.TCP.Port != 6380 {
		t.Fatalf("unexpected config: %+v", cfg.Cache)
	}
}
