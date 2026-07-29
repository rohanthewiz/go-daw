package source

// LiveFeed is the engine's deinterleaved capture buffer for the current
// block. The engine owns exactly one of these; it fills L/R/Frames at the
// top of every duplex callback, *before* any channel processing runs.
// Because the fill and all reads happen synchronously on the same audio
// thread within one callback, no locking is needed — and since duplex mode
// delivers input in the same callback that requests output, live monitoring
// adds zero buffering latency.
type LiveFeed struct {
	L, R   []float32 // capacity = engine maxBlock; valid length = Frames
	Frames int
}

// LiveSource taps the shared LiveFeed. Multiple channels may safely use
// live input simultaneously (e.g. one clean, one heavily effected).
type LiveSource struct {
	feed *LiveFeed
}

// NewLive wraps the engine's capture feed.
func NewLive(feed *LiveFeed) *LiveSource {
	return &LiveSource{feed: feed}
}

// Read copies the current block of captured audio. Audio thread.
func (s *LiveSource) Read(l, r []float32) int {
	n := s.feed.Frames
	if n > len(l) {
		n = len(l)
	}
	copy(l[:n], s.feed.L[:n])
	copy(r[:n], s.feed.R[:n])
	return n
}

func (s *LiveSource) Name() string { return "live" }
