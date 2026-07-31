package ui

import (
	"os"
	"testing"
)

// testBank mirrors the source package's convention: exercise the real bank
// where it exists, skip where it doesn't, so a machine without downloaded
// soundbanks still runs a green suite.
func testBank(t *testing.T) string {
	t.Helper()
	const bank = "../../soundbanks/GeneralUser-GS.sf2"
	if _, err := os.Stat(bank); err != nil {
		t.Skipf("no test bank at %s; skipping bank-backed kit tests", bank)
	}
	return bank
}

// TestDrumKitOptionsFallback covers the two paths that must never yield an
// empty dropdown: no bank selected at all, and a path that can't be parsed.
func TestDrumKitOptionsFallback(t *testing.T) {
	for _, path := range []string{"", "/nonexistent/bank.sf2"} {
		kits := drumKitOptions(path, 0)
		if len(kits) != len(gmDrumKits) {
			t.Errorf("drumKitOptions(%q) = %d entries, want the %d-entry GS fallback",
				path, len(kits), len(gmDrumKits))
		}
	}
}

// TestDrumKitOptionsFromBank verifies the listing comes from the bank itself,
// not the GS table — for GeneralUser-GS the two genuinely differ (it names kit
// 25 "808/909" and adds a kit at 127).
func TestDrumKitOptionsFromBank(t *testing.T) {
	kits := drumKitOptions(testBank(t), 0)
	if len(kits) == 0 {
		t.Fatal("expected kits from the bank")
	}
	sameAsGS := len(kits) == len(gmDrumKits)
	if sameAsGS {
		for i := range kits {
			if kits[i] != gmDrumKits[i] {
				sameAsGS = false
				break
			}
		}
	}
	if sameAsGS {
		t.Error("kit list is identical to the GS fallback; bank presets were not read")
	}
	if kits[0].Prog != 0 {
		t.Errorf("first kit prog = %d, want 0", kits[0].Prog)
	}
}

// TestDrumKitOptionsPlaceholder pins the bank-switch case: a kit the new bank
// lacks still appears (in program order) so the select can keep showing the
// server's real state instead of snapping to another kit.
func TestDrumKitOptionsPlaceholder(t *testing.T) {
	const missing = 30 // between GS 25 and 32; no bank in the repo defines it
	kits := drumKitOptions(testBank(t), missing)

	idx := -1
	for i, k := range kits {
		if k.Prog == missing {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("placeholder for kit %d missing from the list", missing)
	}
	if idx > 0 && kits[idx-1].Prog >= missing {
		t.Errorf("placeholder out of order: %d precedes %d", kits[idx-1].Prog, missing)
	}
	if idx < len(kits)-1 && kits[idx+1].Prog <= missing {
		t.Errorf("placeholder out of order: %d follows %d", kits[idx+1].Prog, missing)
	}

	// A kit the bank does define must not be duplicated by the placeholder path.
	present := drumKitOptions(testBank(t), 0)
	count := 0
	for _, k := range present {
		if k.Prog == 0 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("kit 0 appears %d times, want 1", count)
	}
}
