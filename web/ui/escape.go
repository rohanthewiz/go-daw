package ui

import "html"

// esc HTML-escapes text and attribute values that come from outside the
// code: names people type (scenes, channels via scenes), file paths and
// names from disk (WAVs, banks, songs), and strings from loaded data (SF2
// preset names, plugin module and parameter names).
//
// The element builder writes attribute values and text verbatim, so without
// this a scene called `Bob's "Live" mix` truncated its own <option value> at
// the quote — Recall and Delete then posted a name that does not exist —
// and a name containing markup was stored HTML injected into every page
// load. Compiled-in labels need no escaping and are left alone.
//
// Round-trip: the browser decodes entities, so option.value read by app.js
// is the original string and the server matches it as before.
func esc(s string) string { return html.EscapeString(s) }
