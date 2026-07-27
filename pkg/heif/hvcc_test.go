package heif

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHVCCFromSipsFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sips-quad-64x48.heic"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	c, err := parseContainer(data)
	if err != nil {
		t.Fatalf("parseContainer: %v", err)
	}
	primary, err := c.primary()
	if err != nil {
		t.Fatalf("primary: %v", err)
	}
	if primary.hvcC == nil {
		t.Fatal("primary item has no hvcC property")
	}
	cfg, err := parseHVCC(primary.hvcC)
	if err != nil {
		t.Fatalf("parseHVCC: %v", err)
	}
	if cfg.nalLengthSize != 4 {
		t.Errorf("nalLengthSize = %d, want 4 (Apple default)", cfg.nalLengthSize)
	}
	if len(cfg.ps.SPS) != 1 || len(cfg.ps.PPS) != 1 {
		t.Fatalf("param sets: %d SPS, %d PPS, want 1 each", len(cfg.ps.SPS), len(cfg.ps.PPS))
	}
}

func TestParseHVCCMalformed(t *testing.T) {
	if _, err := parseHVCC(nil); err == nil {
		t.Error("nil hvcC accepted")
	}
	if _, err := parseHVCC([]byte{2}); err == nil {
		t.Error("bad configuration version accepted")
	}
	if _, err := parseHVCC(make([]byte, 23)); err == nil {
		t.Error("hvcC without parameter sets accepted")
	}
}
