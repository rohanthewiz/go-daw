// Package tour defines the built-in getting-started tour: an ordered walk
// across the live mixer page that spotlights each control in turn and explains
// what it does.
//
// Like the tutorial catalog, steps are pure data served as JSON — nothing here
// touches the audio engine or holds runtime state. The browser owns every bit
// of interaction (spotlight geometry, balloon placement, opening collapsed
// sections, resuming across the page reloads that structural edits trigger),
// because all of that is view state that only the DOM can answer.
//
// A step points at a live element by CSS selector rather than at a copy of the
// UI, so the tour can never drift out of sync with a control that moved: if a
// selector stops matching, the client skips that step instead of describing
// something that is not on screen.
package tour

import "github.com/rohanthewiz/serr"

// Step is one stop on the tour.
type Step struct {
	// Section groups consecutive steps under one heading in the balloon
	// ("Transport", "Channel strip"), so a 25-step walk reads as a handful of
	// chapters instead of an undifferentiated count.
	Section string `json:"section"`
	// Target is a CSS selector for the element to spotlight. Empty means a
	// centered card with no spotlight — used for the opening and closing steps,
	// which talk about the app rather than about one control.
	Target string `json:"target"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	// Tip is an optional one-line "try this" rendered under the body. The tour
	// does not block the page, so every tip is something the reader can do
	// right now without leaving the step.
	Tip string `json:"tip,omitempty"`
	// Place is the preferred balloon side ("below", "above", "right", "left").
	// The client treats it as a hint and flips to whichever side actually fits.
	Place string `json:"place,omitempty"`
	// Needs names an optional capability this step depends on; the server drops
	// steps whose capability is absent (see Filter). A step about live input is
	// noise on a playback-only run, and one about SoundFonts is worse than
	// noise when the banks folder is empty — it points at a disabled control.
	Needs string `json:"needs,omitempty"`
}

// Capability values accepted in Step.Needs. Kept as constants (rather than
// bare strings at each use) so the validity check in Filter and the authoring
// vocabulary can never disagree.
const (
	NeedsDuplex    = "duplex"    // engine opened a capture device
	NeedsSoundbank = "soundbank" // at least one .sf2 on disk
	NeedsMidi      = "midi"      // at least one .mid on disk (and a bank to play it through)
	NeedsGroups    = "groups"    // console has at least one group bus
)

// Available describes what the running instance can actually do. The web layer
// fills it from live engine and filesystem state at request time.
type Available struct {
	Duplex    bool
	Soundbank bool
	Midi      bool
	Groups    bool
}

// has reports whether a capability name is satisfied. The bool return
// distinguishes "not satisfied" from "not a capability I know about", which is
// how Filter turns an authoring typo into an error instead of a silently
// vanishing step.
func (a Available) has(need string) (satisfied, known bool) {
	switch need {
	case NeedsDuplex:
		return a.Duplex, true
	case NeedsSoundbank:
		return a.Soundbank, true
	case NeedsMidi:
		return a.Midi, true
	case NeedsGroups:
		return a.Groups, true
	}
	return false, false
}

// Steps returns the full catalog, unfiltered. The slice is shared, not copied —
// callers must treat it as read-only.
func Steps() []Step { return builtin }

// Filter returns the steps this instance should show, in catalog order. An
// unknown Needs value is an authoring bug, so it is reported rather than
// quietly dropped — the handler logs it and falls back to the full catalog.
func Filter(a Available) ([]Step, error) {
	out := make([]Step, 0, len(builtin))
	for _, s := range builtin {
		if s.Needs == "" {
			out = append(out, s)
			continue
		}
		satisfied, known := a.has(s.Needs)
		if !known {
			return nil, serr.New("tour step names an unknown capability",
				"step", s.Title, "needs", s.Needs)
		}
		if satisfied {
			out = append(out, s)
		}
	}
	return out, nil
}

// ch1 addresses a control on the first channel strip. Every strip renders
// identically, so the tour walks one of them and says so — pointing at channel
// 1 keeps the spotlight on screen without horizontal scrolling on any display.
func ch1(sel string) string { return `.strip[data-strip="1"] ` + sel }

// builtin is the tour, ordered as a first-time reader should meet the app:
// what it is, the transport that governs the whole console, one channel strip
// from source to fader, where channels end up (buses), then the two things you
// can play right now (piano, lessons), and finally where to go next.
var builtin = []Step{
	// ---- welcome ----
	{
		Section: "Welcome",
		Title:   "Welcome to go-daw",
		Body: "This is a full mixing console running in Go: eight input channels, " +
			"group buses, a master bus, and a real-time audio engine talking to your " +
			"sound hardware. The browser is only a control surface — every fader move " +
			"is an HTTP request that lands in an atomic cell the audio thread reads, " +
			"so nothing you do here can stall the sound.",
		Tip: "The tour does not lock the page. Try any control as you go — arrow keys move between steps, Esc ends the tour.",
	},
	{
		Section: "Welcome",
		Target:  ".console",
		Place:   "below",
		Title:   "The signal path, left to right",
		Body: "Audio enters at a channel on the left, runs through that channel's " +
			"processing, and lands on a bus on the right. In full: source → gain → gate → " +
			"high-pass → low-pass → EQ → compressor → plugin modules → reverb → pan → " +
			"group or master → safety limiter → your speakers. The next dozen steps walk " +
			"that chain in order.",
	},

	// ---- transport ----
	{
		Section: "Transport",
		Target:  ".transport",
		Place:   "below",
		Title:   "The transport bar",
		Body: "Everything that governs the whole console lives up here and stays " +
			"pinned as you scroll: recording, the metronome, and scene memory. " +
			"Channel-specific controls all live down in the strips.",
	},
	{
		Section: "Transport",
		Target:  "#rec-btn",
		Place:   "below",
		Title:   "Record the master bus",
		Body: "REC taps the master bus — everything you hear, after the limiter — and " +
			"writes it to a timestamped WAV in the recordings folder. It starts after a " +
			"one-bar count-in at the metronome tempo, so you have a moment to get ready. " +
			"Press it again during the count to abort before anything is captured.",
		Tip: "Shift-click REC to skip the count-in and start instantly — handy for catching something already playing.",
	},
	{
		Section: "Transport",
		Target:  "#metro-btn",
		Place:   "below",
		Title:   "The metronome",
		Body: "CLICK starts and stops the beat. The browser keeps the clock and each " +
			"beat asks the engine for one short tone, which is mixed into your monitor " +
			"path only — clicks are never printed into a recording. The button flashes on " +
			"every beat, brighter on the downbeat.",
		Tip: "Don't know the tempo? Tap the CLICK button in time three or more times and it sets the BPM from your hand.",
	},
	{
		Section: "Transport",
		Target:  ".metro-box",
		Place:   "below",
		Title:   "Tempo and meter",
		Body: "Set beats per minute (30–300) and beats per bar (2/4, 3/4, 4/4, 6/8). " +
			"Both are re-read on every beat, so edits take effect within a beat — no stop " +
			"and restart. Both persist to the database, so your tempo is still here after " +
			"a restart, and both feed the record and lesson count-ins.",
	},
	{
		Section: "Transport",
		Target:  ".scene-box",
		Place:   "left",
		Title:   "Scenes: the console's memory",
		Body: "A scene is a snapshot of the entire console — every source, filter, EQ " +
			"band, compressor setting, module and its parameters, routing, pan, and " +
			"fader. Type a name and press Save. Recall applies a saved scene live, " +
			"through the same atomic cells the faders use, so the audio never stops.",
		Tip: "Save a scene called \"start\" before you experiment. Recall is then always one click away.",
	},
	{
		Section: "Transport",
		Target:  "#conn-dot",
		Place:   "left",
		Title:   "The meter stream",
		Body: "This dot goes green while the browser is receiving the server-sent event " +
			"stream that drives every meter on the page, about twelve times a second. If " +
			"it goes grey, the meters are frozen but the audio engine is still running.",
	},

	// ---- channel strip ----
	{
		Section: "Channel strip",
		Target:  `.strip[data-strip="1"]`,
		Place:   "right",
		Title:   "One channel, top to bottom",
		Body: "Every one of the eight channels is identical, so the tour walks this one. " +
			"Reading downward you get: the source, then the processing sections, then " +
			"routing, mute, and the fader with its meter. Sections you are not using stay " +
			"folded so a strip is still scannable at a glance.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(`select[data-role="source-select"]`),
		Place:   "right",
		Title:   "Src: where the sound comes from",
		Body: "Each channel gets its audio from exactly one source. The choices are a " +
			"test oscillator, a polyphonic synth, a sampled SoundFont instrument, a MIDI " +
			"song player, your live input, or a WAV file. Changing this rebuilds the " +
			"source and reloads the page — the server stays the single source of truth.",
		Tip: "Go ahead and switch it. The tour remembers where you were and picks up on the reloaded page.",
	},
	{
		Section: "Channel strip",
		Target:  `.src-osc[data-id="1"]`,
		Place:   "right",
		Title:   "The test oscillator",
		Body: "Channel 1 starts on a quiet 220 Hz sine so a fresh launch makes sound " +
			"immediately. Freq sweeps 20 Hz to 5 kHz on a logarithmic slider — linear " +
			"would squeeze the whole bass register into the first few pixels — and Lvl " +
			"trims it. This is the fastest way to prove your speakers and metering work.",
		Tip: "Raise Lvl and watch the channel meter and the master meter move together.",
	},
	{
		Section: "Channel strip",
		Target:  `.strip[data-strip="4"]`,
		Place:   "right",
		Needs:   NeedsSoundbank,
		Title:   "Sampled instruments",
		Body: "This channel is running a SoundFont — real recorded samples rather than " +
			"synthesis. Pick a bank and any of the 128 General MIDI instruments; program " +
			"changes ride the synth's event ring, so switching instruments never " +
			"interrupts a sounding note. Toggle Drums to reroute notes to the percussion " +
			"channel and choose a kit.",
		Tip: "Compare it with the additive synth on channel 3 — same keyboard, very different sound.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(`select[data-role="source-select"]`),
		Place:   "right",
		Needs:   NeedsMidi,
		Title:   "Playing a MIDI song",
		Body: "Choosing \"midi\" turns a channel into a song player: pick a bank to " +
			"render through and a .mid file to play, then use the play/pause/stop " +
			"transport that appears. The position readout rides the same meter stream as " +
			"everything else, so every open tab agrees on where the song is.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(`select[data-role="source-select"]`),
		Place:   "right",
		Needs:   NeedsDuplex,
		Title:   "Live input",
		Body: "\"live\" feeds this channel from your microphone or audio interface in " +
			"full duplex. If the engine could not open a capture device the option is " +
			"disabled and a playback-only badge appears in the transport bar.",
		Tip: "Monitor live input on headphones. Speakers will feed straight back into the mic.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(`details[data-sect="gate"]`),
		Place:   "right",
		Title:   "Gate: silence between the notes",
		Body: "A noise gate mutes the channel while the signal sits below a threshold, " +
			"which kills hiss and room tone in the gaps. Hysteresis sets how much quieter " +
			"it must get before closing again, so a signal hovering at the threshold does " +
			"not chatter, and attack/hold/release shape how quickly it moves.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(`details[data-sect="filters"]`),
		Place:   "right",
		Title:   "Filters: trimming the extremes",
		Body: "A high-pass filter removes everything below its corner frequency — the " +
			"standard cure for rumble, handling noise, and footsteps on a vocal mic. A " +
			"low-pass does the same at the top for hiss and harshness. Both frequency " +
			"sliders are logarithmic, matching how you hear pitch.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(`details[data-sect="eq"]`),
		Place:   "right",
		Title:   "EQ: three parametric bands",
		Body: "Low, Mid, and High are full parametric bands built on RBJ biquads. Frq " +
			"picks the center frequency, Gain boosts or cuts it by up to 18 dB, and Q " +
			"sets how wide the affected region is — low Q for a broad tone shift, high Q " +
			"to notch out one ringing frequency.",
		Tip: "Cutting usually sounds better than boosting. Try a small cut where a sound feels crowded.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(`details[data-sect="comp"]`),
		Place:   "right",
		Title:   "Compressor: evening out the level",
		Body: "Above the threshold, the compressor turns the signal down by the ratio — " +
			"4:1 means four decibels in produce one decibel out. Knee softens the corner, " +
			"attack and release set how fast it reacts, and Makeup restores the level you " +
			"lost. Push the ratio to 20 or beyond and it works as a hard limiter.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(`details[data-sect="reverb"]`),
		Place:   "right",
		Title:   "Reverb: putting it in a room",
		Body: "A Schroeder/Freeverb network with a configurable pre-delay — the gap " +
			"before the first reflections arrives, which is what your ear reads as room " +
			"size. Decay sets how long the tail rings, Damp rolls the highs off as it " +
			"decays, and Mix balances dry against wet.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(`details[data-sect="modules"]`),
		Place:   "right",
		Title:   "Modules: the plugin chain",
		Body: "Modules are insert effects, processed in order, sitting between the " +
			"compressor and the reverb. Tremolo and delay are built in; anything dropped " +
			"into the plugins folder as a compiled Go plugin joins the same list. Every " +
			"slider you see was generated from the module's own parameter metadata — " +
			"there is no per-plugin UI code anywhere.",
		Tip: "Writing your own is one Go file exporting NewModule(). The README walks through a complete example.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(`input[data-param="pan"]`),
		Place:   "right",
		Title:   "Pan",
		Body: "Places the channel between the speakers using a constant-power law, so a " +
			"sound keeps the same perceived loudness as it crosses the stereo field " +
			"instead of dipping in the middle. Like the fader, it ramps across each block " +
			"rather than jumping, which is what keeps moves free of zipper noise.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(".route-row"),
		Place:   "right",
		Needs:   NeedsGroups,
		Title:   "Bus: where this channel goes",
		Body: "Send the channel straight to Master, or to one of the group buses. " +
			"Groups let you ride a whole submix — all the drums, all the backing vocals — " +
			"on one fader, and mute the lot with one button.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(".mute-btn"),
		Place:   "right",
		Title:   "Mute",
		Body: "Silences this channel at its output. The source keeps running and the " +
			"meter keeps moving, so unmuting drops you back into a song already in " +
			"progress rather than starting it over.",
	},
	{
		Section: "Channel strip",
		Target:  ch1(".fader-row"),
		Place:   "right",
		Title:   "Fader and meter",
		Body: "The fader runs from −60 dB to +12 dB, with 0 as unity gain. Beside it, " +
			"the meter shows RMS as the filled bar and peak as the floating white tick, " +
			"scaled so the bottom is −60 dBFS and the top is 0. Peaks touching the top " +
			"are the ones to watch.",
	},

	// ---- buses ----
	{
		Section: "Buses",
		Target:  ".strip-group",
		Place:   "left",
		Needs:   NeedsGroups,
		Title:   "Group buses",
		Body: "Four submix buses, each with its own fader, mute, and meter. Every " +
			"channel routed here is summed before the group fader, which then feeds the " +
			"master. One move balances a whole section against the rest of the mix.",
	},
	{
		Section: "Buses",
		Target:  ".strip-master",
		Place:   "left",
		Title:   "Master bus",
		Body: "Everything ends here: groups, direct channels, and the metronome click. " +
			"An always-on safety limiter sits after the master fader so a runaway level " +
			"cannot reach your speakers as a full-scale square wave. This is also the " +
			"exact signal the recorder captures — the click excepted.",
	},

	// ---- piano ----
	{
		Section: "Piano",
		Target:  "#piano",
		Place:   "above",
		Title:   "The virtual piano",
		Body: "Twenty-five keys — two octaves plus the top C — driving whichever " +
			"channel is running a playable instrument. It is a real instrument, not a " +
			"demo: notes go straight into the synth's lock-free event ring, so they arrive " +
			"without ever making the audio thread wait.",
	},
	{
		Section: "Piano",
		Target:  "#piano-channel",
		Place:   "above",
		Title:   "Choosing what to play",
		Body: "Pick the channel the keys should sound on. Channels already running an " +
			"instrument are listed plainly; picking one that isn't installs a synth on it " +
			"first, which reloads the page. Because the piano just targets a channel, it " +
			"inherits that channel's whole processing chain — EQ, compressor, reverb, " +
			"modules, fader, and all.",
		Tip: "Add reverb on the piano channel, then play. You are hearing the same DSP a live mic would go through.",
	},
	{
		Section: "Piano",
		Target:  "#piano-keys",
		Place:   "above",
		Title:   "Four ways to play",
		Body: "Click a key — striking low on the key plays louder, the way a real key " +
			"travels further — or drag across keys for a glissando. Or use the computer " +
			"keyboard: A S D F for white keys, W E T Y U for the sharps, Z and X to shift " +
			"octave, space bar for the sustain pedal.",
		Tip: "The letters printed on each key are its computer-keyboard shortcut.",
	},
	{
		Section: "Piano",
		Target:  "#midi-box",
		Place:   "above",
		Title:   "A hardware keyboard",
		Body: "Plug in a MIDI keyboard and it appears in this picker; the dot turns " +
			"green once it is bound. Hot-plug works both ways, velocity and the sustain " +
			"pedal come through, and unplugging mid-note releases everything that device " +
			"was holding rather than leaving a note ringing.",
	},

	// ---- lessons ----
	{
		Section: "Lessons",
		Target:  "#tutorial",
		Place:   "above",
		Title:   "Guided piano lessons",
		Body: "Seven built-in lessons take you from finding middle C to playing the " +
			"I–V–vi–IV progression: scales, Mary Had a Little Lamb, Twinkle Twinkle, Ode " +
			"to Joy, and triads. Every input route counts — on-screen keys, computer " +
			"keyboard, or hardware MIDI.",
	},
	{
		Section: "Lessons",
		Target:  "#tut-demo",
		Place:   "above",
		Title:   "Listen first, then play",
		Body: "Listen plays the lesson through your instrument so you know how it goes " +
			"before trying it. Start then arms the lesson: the next note is ringed in " +
			"amber on the keyboard, a wrong key flashes red and is counted, and chord " +
			"steps only advance once every ringed key is held at the same time.",
		Tip: "Both begin with a four-beat count-in at the metronome tempo. Uncheck Count-in to start on the spot.",
	},
	{
		Section: "Lessons",
		Target:  "#tut-strip",
		Place:   "above",
		Title:   "Following along",
		Body: "One chip per step, scrolling like a score: finished steps turn green, the " +
			"current one is amber. Completed lessons keep a check mark and a lifetime " +
			"record — how many times you have passed, and your fewest misses — stored in " +
			"the same database as your scenes.",
	},

	// ---- next ----
	{
		Section: "Next",
		Title:   "That's the console",
		Body: "A good first session: put the metronome on, pick an instrument on the " +
			"piano channel, run lesson 1, then hit REC and keep the take. Recordings land " +
			"in the recordings folder as WAV; drop .sf2 banks into soundbanks and .mid " +
			"files into midifiles and they appear in the selectors on the next reload.",
		Tip: "Press Tour in the transport bar to run this again whenever you want.",
	},
}
