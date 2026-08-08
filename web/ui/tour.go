package ui

import "github.com/rohanthewiz/element"

// TourOverlay is the getting-started tour's shell: a spotlight ring and a
// balloon, both empty and hidden until app.js drives them. Like the tutorial
// panel, the server renders only structure — the step text arrives from
// /api/tour and every position is view state (element geometry, viewport size,
// scroll offset) that only the browser can compute.
//
// The overlay deliberately does not block the page. The ring is drawn with a
// huge outward box-shadow, which dims everything around the target while
// leaving the target itself clickable, so a reader can work the control being
// described instead of only reading about it. Structural edits still reload the
// page, so the client stashes its position and resumes on the way back in.
//
//	┌ ◈ Channel strip · 12 / 31 ──────────── × ┐
//	│ EQ: three parametric bands               │
//	│ Low, Mid, and High are full parametric…  │
//	│ ▸ Cutting usually sounds better than…    │
//	│ ●●●●●○○○○○○      [Back]  [Next →]        │
//	└──────────────────────────────────────────┘
type TourOverlay struct {
	// Seen renders server-side (like the tutorial's count-in checkbox) rather
	// than being fetched after load, so the auto-start decision on a first
	// visit is made before paint and the tour never flashes open on a return
	// visit that had already dismissed it.
	Seen bool
}

func (t TourOverlay) Render(b *element.Builder) (x any) {
	attrs := []string{"id", "tour"}
	if t.Seen {
		attrs = append(attrs, "data-seen", "1")
	}

	b.DivClass("tour", attrs...).R(
		b.DivClass("tour-ring", "id", "tour-ring").R(),

		b.DivClass("tour-balloon", "id", "tour-balloon").R(
			b.DivClass("tour-head").R(
				b.SpanClass("tour-section", "id", "tour-section").R(),
				b.SpanClass("tour-count", "id", "tour-count").R(),
				b.ButtonClass("tour-close", "id", "tour-close", "title", "End the tour (Esc)").T("×"),
			),
			b.DivClass("tour-title", "id", "tour-title").R(),
			b.DivClass("tour-body", "id", "tour-body").R(),
			b.DivClass("tour-tip", "id", "tour-tip").R(),
			b.DivClass("tour-foot").R(
				// One dot per step, painted by app.js. It doubles as a jump
				// control — on a thirty-step tour, "back to the piano" should
				// not mean twelve presses of Back.
				b.DivClass("tour-dots", "id", "tour-dots").R(),
				b.ButtonClass("tour-back", "id", "tour-back", "title", "Previous step (←)").T("Back"),
				b.ButtonClass("tour-next", "id", "tour-next", "title", "Next step (→)").T("Next"),
			),
		),
	)
	return
}
