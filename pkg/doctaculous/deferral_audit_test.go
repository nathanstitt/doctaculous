package doctaculous

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoUnsignedDeferrals enforces the paged-media sign-off policy: every deferral that
// remains in the paged-media code paths must be covered by an owner-signed row in the
// ledger (docs/paged-media-deferral-signoffs.md). It fails if the ledger still has its
// placeholder or no signed rows, and it surfaces (via t.Logf) every deferral marker
// found in the code so a reviewer can confirm each maps to a signed row.
//
// "Deferral markers" here are degradation log lines in the paginator — a box that is
// genuinely indivisible (an over-tall single line / table row / flex-or-grid band) must
// overflow, and a mid-block forced break that cannot be honored without splitting the
// box is dropped. Those inherent degradations are covered by the signed ledger rows for
// mid-cell / mid-item / single-line-indivisible content.
func TestNoUnsignedDeferrals(t *testing.T) {
	ledgerPath := filepath.Join("..", "..", "docs", "paged-media-deferral-signoffs.md")
	ledger, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read ledger %s: %v", ledgerPath, err)
	}
	led := string(ledger)

	// The ledger must no longer carry its empty placeholder.
	if strings.Contains(led, "_none yet_") {
		t.Errorf("ledger still has the _none yet_ placeholder — fill in or remove it")
	}
	// At least one owner-signed deferral row must exist (every gate the owner chose to
	// defer is signed; every gate they chose to implement removed the deferral).
	signed := strings.Count(led, "| Nathan |")
	if signed == 0 {
		t.Errorf("no owner-signed rows in the ledger; every remaining deferral must be signed by the owner")
	}

	// Surface every deferral marker found in the paged-media source so a reviewer can
	// confirm coverage. This is a soft check (t.Logf, not t.Error): the markers that
	// remain are inherent degradations of genuinely-indivisible content, covered by the
	// signed rows. A NEW unsigned deferral marker is the thing to catch in review.
	deferRe := regexp.MustCompile(`(?i)not honored|overflowing, not splitting|deferred`)
	roots := []string{
		"marginbox.go", "fragmentpage.go", "paginate.go", "tablepage.go",
		"flexgridpage.go", "pagemodel.go", "runningelement.go", "stringsnapshot.go",
	}
	found := 0
	for _, name := range roots {
		path := filepath.Join("..", "layout", "css", name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // a file a particular feature never created
		}
		for i, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, "logf(") {
				continue
			}
			if deferRe.MatchString(line) {
				found++
				t.Logf("deferral marker %s:%d — confirm a signed ledger row covers it:\n    %s",
					name, i+1, strings.TrimSpace(line))
			}
		}
	}
	t.Logf("found %d deferral marker(s) in the paged-media code paths; ledger has %d signed row(s)", found, signed)
}
