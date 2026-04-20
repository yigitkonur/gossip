package main

import (
	"os"
	"testing"

	"github.com/yigitkonur/gossip/internal/statedir"
)

func TestResolvedControlPort_UsesPortsFileWhenPresent(t *testing.T) {
	sd := statedir.New(t.TempDir())
	if err := os.WriteFile(sd.PortsFile(), []byte("{\n  \"controlPort\": 4702,\n  \"appPort\": 4700,\n  \"proxyPort\": 4701\n}\n"), 0o644); err != nil {
		t.Fatalf("write ports file: %v", err)
	}

	if got := resolvedControlPort(sd); got != 4702 {
		t.Fatalf("resolvedControlPort() = %d, want 4702", got)
	}
}

func TestResolvedControlPort_FallsBackToEnvDefault(t *testing.T) {
	t.Setenv("GOSSIP_CONTROL_PORT", "4802")
	sd := statedir.New(t.TempDir())
	if got := resolvedControlPort(sd); got != 4802 {
		t.Fatalf("resolvedControlPort() = %d, want 4802", got)
	}
}
