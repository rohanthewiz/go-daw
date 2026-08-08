package tour

import (
	"strings"
	"testing"
)

// TestCatalogInvariants checks what the client assumes about every step: it
// always has something to render, its placement hint is one the balloon
// positioner understands, and a placement hint is never attached to a step
// with nothing to place against.
func TestCatalogInvariants(t *testing.T) {
	places := map[string]bool{"": true, "below": true, "above": true, "right": true, "left": true}

	for _, s := range Steps() {
		if s.Section == "" || s.Title == "" || s.Body == "" {
			t.Errorf("step %q: missing section, title, or body", s.Title)
		}
		if !places[s.Place] {
			t.Errorf("step %q: unknown placement %q", s.Title, s.Place)
		}
		if s.Target == "" && s.Place != "" {
			t.Errorf("step %q: placement %q on a step with no target", s.Title, s.Place)
		}
		// A targetless step renders as a centered card, which only reads well
		// as an opening or closing remark — one buried mid-tour would look
		// like a spotlight that failed to land.
		if s.Target == "" && s.Section != "Welcome" && s.Section != "Next" {
			t.Errorf("step %q: targetless card outside the Welcome/Next sections", s.Title)
		}
	}
}

// TestSectionsAreContiguous pins the grouping the balloon header relies on:
// section names label runs of steps, so a name that reappears after another
// section would show the reader "Transport" twice with a gap between.
func TestSectionsAreContiguous(t *testing.T) {
	seen := map[string]bool{}
	prev := ""
	for _, s := range Steps() {
		if s.Section == prev {
			continue
		}
		if seen[s.Section] {
			t.Errorf("section %q resumes after %q; sections must be contiguous", s.Section, prev)
		}
		seen[s.Section] = true
		prev = s.Section
	}
}

// TestFilterCapabilities covers both ends: everything available yields the
// whole catalog, nothing available yields exactly the unconditional steps.
func TestFilterCapabilities(t *testing.T) {
	all, err := Filter(Available{Duplex: true, Soundbank: true, Midi: true, Groups: true})
	if err != nil {
		t.Fatalf("Filter with all capabilities: %v", err)
	}
	if len(all) != len(Steps()) {
		t.Errorf("Filter with all capabilities returned %d steps, want the full catalog of %d",
			len(all), len(Steps()))
	}

	none, err := Filter(Available{})
	if err != nil {
		t.Fatalf("Filter with no capabilities: %v", err)
	}
	for _, s := range none {
		if s.Needs != "" {
			t.Errorf("step %q needs %q but survived an empty Available", s.Title, s.Needs)
		}
	}

	want := 0
	for _, s := range Steps() {
		if s.Needs == "" {
			want++
		}
	}
	if len(none) != want {
		t.Errorf("Filter with no capabilities returned %d steps, want %d", len(none), want)
	}
}

// TestFilterRejectsUnknownCapability guards the authoring vocabulary: a typo in
// Needs must surface as an error, not as a step that silently never shows.
func TestFilterRejectsUnknownCapability(t *testing.T) {
	saved := builtin
	defer func() { builtin = saved }()

	builtin = []Step{{Section: "Welcome", Title: "typo", Body: "x", Needs: "duplexx"}}
	if _, err := Filter(Available{Duplex: true}); err == nil {
		t.Fatal("Filter accepted an unknown capability name")
	}
}

// TestChannelSelectorsTargetChannelOne keeps the strip walk on one strip. The
// tour says "every channel is identical, so we walk this one"; a selector that
// wandered to another strip would make the spotlight jump sideways mid-section
// for no reason the reader can see.
func TestChannelSelectorsTargetChannelOne(t *testing.T) {
	for _, s := range Steps() {
		if s.Section != "Channel strip" || !strings.HasPrefix(s.Target, ".strip[") {
			continue
		}
		// The SoundFont step is the deliberate exception: it points at the
		// channel main.go installs a sampled instrument on.
		if s.Needs == NeedsSoundbank {
			continue
		}
		if !strings.HasPrefix(s.Target, `.strip[data-strip="1"]`) {
			t.Errorf("step %q targets %q; strip-walk steps should stay on channel 1", s.Title, s.Target)
		}
	}
}
