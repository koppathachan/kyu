package engine_test

import (
	"flag"
	"testing"
	"time"

	"github.com/koppathachan/kyu/internal/engine"
)

func TestConfigReadsTheEnvironment(t *testing.T) {
	t.Setenv("KYU_STORAGE", "storage:9999")
	t.Setenv("KYU_SWEEP_INTERVAL", "250ms")
	t.Setenv("KYU_SWEEP_INTERVAL_MAX", "45s")
	t.Setenv("KYU_SCAN_LIMIT", "25")
	t.Setenv("KYU_DISCOVERY_INTERVAL", "90s")
	t.Setenv("KYU_NEGATIVE_CACHE_TTL", "2s")

	cfg, err := engine.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := engine.Config{
		Listen:            "127.0.0.1:50051",
		StorageTarget:     "storage:9999",
		SweepInterval:     250 * time.Millisecond,
		SweepIntervalMax:  45 * time.Second,
		ScanLimit:         25,
		DiscoveryInterval: 90 * time.Second,
		NegativeCacheTTL:  2 * time.Second,
	}
	if cfg != want {
		t.Fatalf("configuration is %+v, want %+v", cfg, want)
	}
}

func TestAFlagOverridesTheEnvironment(t *testing.T) {
	t.Setenv("KYU_STORAGE", "storage:9999")
	t.Setenv("KYU_SCAN_LIMIT", "25")

	cfg, err := engine.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	fs := flag.NewFlagSet("kyu", flag.ContinueOnError)
	cfg.RegisterFlags(fs)
	if err := fs.Parse([]string{"-storage", "elsewhere:1", "-scan-limit", "7"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	if cfg.StorageTarget != "elsewhere:1" {
		t.Fatalf("storage target is %q, want the flag's value", cfg.StorageTarget)
	}
	if cfg.ScanLimit != 7 {
		t.Fatalf("scan limit is %d, want the flag's value", cfg.ScanLimit)
	}
}

// A variable that is set but unreadable is an error, not a silent fallback: a
// deployment that meant to set a one-second sweep and typed it wrong should be
// told, not quietly given thirty.
func TestAnUnreadableEnvironmentVariableIsRefused(t *testing.T) {
	t.Setenv("KYU_SWEEP_INTERVAL", "every so often")
	if _, err := engine.LoadConfig(); err == nil {
		t.Fatal("an unparsable duration was accepted")
	}
}

func TestValidateRefusesAConfigurationThatCannotServe(t *testing.T) {
	cfg := engine.DefaultConfig()
	if err := cfg.Validate(); err == nil {
		t.Fatal("a configuration naming no storage server was accepted; a deployment must supply one")
	}

	cfg.StorageTarget = "storage:1"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("the defaults plus a storage target were refused: %v", err)
	}

	cfg.SweepIntervalMax = time.Millisecond
	if err := cfg.Validate(); err == nil {
		t.Fatal("a backoff ceiling below its floor was accepted")
	}
}

// New refuses a configuration Validate refuses, rather than starting an engine
// that cannot reach anything.
func TestNewRefusesAnInvalidConfiguration(t *testing.T) {
	if _, err := engine.New(engine.DefaultConfig()); err == nil {
		t.Fatal("an engine was built with no storage server configured")
	}
}
