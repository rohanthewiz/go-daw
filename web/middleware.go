package web

import (
	"fmt"
	"net/url"
	"runtime/debug"

	"github.com/rohanthewiz/logger"
	"github.com/rohanthewiz/rweb"
	"github.com/rohanthewiz/serr"
)

// recoverPanics turns a panicking handler into a logged 500.
//
// rweb has no recover() of its own, and this process is also the audio
// engine: a panic in any request goroutine would take the console, every
// open tab, and an in-progress recording (unfinalized header) down with it.
// Panics reachable from a request are real — decoding a malformed WAV used
// to divide by zero — and the right blast radius for one is that request.
//
// It must be the first middleware so it wraps everything after it. It cannot
// protect what rweb does before any handler runs (reading the request body),
// which is rweb's own responsibility.
func recoverPanics(ctx rweb.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.LogErr(serr.New("request handler panicked",
				"method", ctx.Request().Method(), "path", ctx.Request().Path(),
				"panic", fmt.Sprint(r), "stack", string(debug.Stack())))
			err = ctx.WriteError(serr.New("internal error (logged on the server)"), 500)
		}
	}()
	return ctx.Next()
}

// sameOriginWrites rejects state-changing requests that a browser reports as
// coming from another site.
//
// Every control in this app is a JSON POST from its own page. rweb does not
// check Content-Type, so without this any web page the user visits could
// fire "no-cors" text/plain POSTs at the console — rename scenes, start a
// recording, load files by path. Browsers attach Origin (and Sec-Fetch-Site)
// to cross-origin POSTs and pages cannot forge them, so comparing Origin's
// host with the request's Host is a sufficient CSRF check here, with no
// tokens to thread through the UI.
//
// Requests without an Origin header (curl, scripts, the smoke tests) are
// let through: they are not a browser acting on a user's behalf. GETs are
// read-only and never checked.
func sameOriginWrites(ctx rweb.Context) error {
	req := ctx.Request()
	if req.Method() == "GET" || req.Method() == "HEAD" || req.Method() == "OPTIONS" {
		return ctx.Next()
	}
	if req.Header("Sec-Fetch-Site") == "cross-site" {
		return ctx.WriteError(serr.New("cross-site request refused"), 403)
	}
	if origin := req.Header("Origin"); origin != "" {
		// Compare against the Host *header*. rweb's req.Host() is parsed
		// from the request-line URL, which for ordinary (origin-form)
		// requests is just the path, so it reads "localhost" regardless of
		// how the page was reached and would reject every legitimate POST.
		u, err := url.Parse(origin)
		if err != nil || u.Host != req.Header("Host") {
			return ctx.WriteError(serr.New("cross-origin request refused", "origin", origin), 403)
		}
	}
	return ctx.Next()
}
