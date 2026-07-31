package audio

import (
	"sync/atomic"

	"github.com/gen2brain/malgo"
	"github.com/rohanthewiz/go-daw/config"
	"github.com/rohanthewiz/go-daw/mixer"
	"github.com/rohanthewiz/go-daw/record"
	"github.com/rohanthewiz/logger"
	"github.com/rohanthewiz/serr"
)

// maxBlock is the largest frame count one callback invocation will process
// in a single pass. CoreAudio honors the requested period size in practice,
// but the API does not guarantee frameCount == PeriodSizeInFrames, so the
// callback defensively processes oversized requests in maxBlock sub-chunks
// rather than overrunning the preallocated scratch buffers.
const maxBlock = 4096

// Engine owns the audio device and drives the mixer console once per
// hardware period.
//
//	          CoreAudio realtime thread
//	┌─────────────────────────────────────────────┐
//	│ onData(pOut, pIn, frames)                   │
//	│   pIn ─► LiveFeed (deinterleaved)           │
//	│   for each Channel: ProcessBlock ─► sum     │
//	│   for each Group:   ProcessBlock ─► sum     │
//	│   Master.ProcessBlock ─► recorder? ─► pOut  │
//	└─────────────────────────────────────────────┘
//
// Everything the callback touches is either preallocated, atomic, or owned
// exclusively by the audio thread. See the package docs for the realtime
// contract.
type Engine struct {
	Console *mixer.Console

	cfg    *config.Config
	mctx   *malgo.AllocatedContext
	device *malgo.Device

	// Duplex reports whether capture is actually active (may be false even
	// when requested, if the device or mic permission refused duplex init).
	Duplex bool

	// recorder is nil when not recording; the callback checks it wait-free
	// every block.
	recorder atomic.Pointer[record.Recorder]

	// click is the metronome tick generator; triggered from the control
	// plane, rendered by the callback into the monitor path only.
	click *clickGen

	// interleaved scratch for the master output + recorder push
	outScratch []float32
}

// NewEngine builds the console and scratch buffers. Device init happens in
// Start so construction can't trigger a mic-permission prompt.
func NewEngine(cfg *config.Config) *Engine {
	return &Engine{
		Console:    mixer.NewConsole(cfg.ChannelCount, cfg.GroupCount, float64(cfg.SampleRate), maxBlock),
		cfg:        cfg,
		click:      newClickGen(float64(cfg.SampleRate)),
		outScratch: make([]float32, maxBlock*2),
	}
}

// Start opens the miniaudio context and device. If duplex init fails (no
// input device, mic permission denied, exotic hardware), it falls back to
// playback-only rather than refusing to run — a mixer with no live input is
// still a fully functional playback/bounce console.
func (e *Engine) Start() error {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(msg string) {
		// miniaudio logs are chatty at init; Debug level keeps them
		// available without spamming normal runs.
		logger.Debug("miniaudio", "msg", msg)
	})
	if err != nil {
		return serr.Wrap(err, "msg", "initializing miniaudio context")
	}
	e.mctx = mctx

	if e.cfg.Duplex {
		if err := e.initDevice(malgo.Duplex); err != nil {
			logger.LogErr(err, "msg", "duplex init failed; falling back to playback-only (check mic permission in System Settings > Privacy & Security > Microphone)")
		} else {
			e.Duplex = true
		}
	}
	if e.device == nil {
		if err := e.initDevice(malgo.Playback); err != nil {
			e.teardownContext()
			return serr.Wrap(err, "msg", "playback device init failed")
		}
	}

	if err := e.device.Start(); err != nil {
		e.device.Uninit()
		e.teardownContext()
		return serr.Wrap(err, "msg", "starting audio device")
	}

	logger.Info("Audio engine running",
		"sampleRate", itoa(e.cfg.SampleRate),
		"blockFrames", itoa(e.cfg.BlockSize),
		"duplex", boolStr(e.Duplex))
	return nil
}

func (e *Engine) initDevice(devType malgo.DeviceType) error {
	devCfg := malgo.DefaultDeviceConfig(devType)
	devCfg.SampleRate = uint32(e.cfg.SampleRate)
	devCfg.PeriodSizeInFrames = uint32(e.cfg.BlockSize)
	devCfg.Playback.Format = malgo.FormatF32
	devCfg.Playback.Channels = 2
	if devType == malgo.Duplex {
		devCfg.Capture.Format = malgo.FormatF32
		devCfg.Capture.Channels = 2
	}

	dev, err := malgo.InitDevice(e.mctx.Context, devCfg, malgo.DeviceCallbacks{Data: e.onData})
	if err != nil {
		return serr.Wrap(err)
	}
	e.device = dev
	return nil
}

// Stop halts the device and releases the context. Any active recording is
// finalized first so the file gets a valid header.
func (e *Engine) Stop() {
	if rec := e.recorder.Swap(nil); rec != nil {
		rec.Stop()
	}
	if e.device != nil {
		e.device.Uninit()
		e.device = nil
	}
	e.teardownContext()
	logger.Info("Audio engine stopped")
}

func (e *Engine) teardownContext() {
	if e.mctx != nil {
		_ = e.mctx.Uninit()
		e.mctx.Free()
		e.mctx = nil
	}
}

// StartRecording arms the master-bus tap.
func (e *Engine) StartRecording() (*record.Recorder, error) {
	if e.recorder.Load() != nil {
		return nil, serr.New("already recording")
	}
	rec, err := record.Start(e.cfg.RecordingsDir, e.cfg.SampleRate)
	if err != nil {
		return nil, serr.Wrap(err)
	}
	e.recorder.Store(rec)
	return rec, nil
}

// StopRecording detaches the tap FIRST (so the callback stops pushing),
// then finalizes the file. Returns the path and recorded seconds.
func (e *Engine) StopRecording() (string, float64, error) {
	rec := e.recorder.Swap(nil)
	if rec == nil {
		return "", 0, serr.New("not recording")
	}
	path, seconds := rec.Stop()
	return path, seconds, nil
}

// Recorder returns the active recorder or nil.
func (e *Engine) Recorder() *record.Recorder { return e.recorder.Load() }

// TriggerClick queues one metronome click (accent = bar-start voicing). The
// browser owns the tempo clock and calls this once per beat; the engine only
// makes the sound, at the next block boundary (≤ one period of latency).
func (e *Engine) TriggerClick(accent bool) { e.click.Trigger(accent) }

// onData is the miniaudio data callback — the engine's heartbeat, running
// on the CoreAudio realtime thread. pIn is nil in playback-only mode.
func (e *Engine) onData(pOut, pIn []byte, frameCount uint32) {
	remaining := int(frameCount)
	offset := 0
	outAll := f32View(pOut, remaining, 2)
	var inAll []float32
	if pIn != nil {
		inAll = f32View(pIn, remaining, 2)
	}

	// Sub-chunk loop: normally executes exactly once (frameCount ==
	// requested period), but guards the scratch buffers if the OS asks for
	// a giant buffer during e.g. a device switch.
	for remaining > 0 {
		n := remaining
		if n > maxBlock {
			n = maxBlock
		}
		e.processBlock(outAll, inAll, offset, n)
		offset += n
		remaining -= n
	}
}

func (e *Engine) processBlock(outAll, inAll []float32, offset, n int) {
	con := e.Console

	// 1) Publish this block's capture into the LiveFeed so LiveSources can
	//    read it during channel processing (same thread, same block).
	feed := con.LiveFeed
	if inAll != nil {
		deinterleave(inAll[offset*2:], feed.L, feed.R, n)
		feed.Frames = n
	} else {
		feed.Frames = 0
	}

	// 2) Clear all bus accumulators.
	for _, g := range con.Groups {
		g.Clear(n)
	}
	con.Master.Clear(n)

	// 3) Render channels and sum each into its routing target.
	for _, ch := range con.Channels {
		l, r := ch.ProcessBlock(n)

		dstL, dstR := con.Master.L, con.Master.R
		if gi := int(ch.GroupIdx.Load()); gi >= 1 && gi <= len(con.Groups) {
			dstL, dstR = con.Groups[gi-1].L, con.Groups[gi-1].R
		}
		for i := 0; i < n; i++ {
			dstL[i] += l[i]
			dstR[i] += r[i]
		}
	}

	// 4) Groups apply their gain/mute, then feed the master.
	for _, g := range con.Groups {
		g.ProcessBlock(n)
		for i := 0; i < n; i++ {
			con.Master.L[i] += g.L[i]
			con.Master.R[i] += g.R[i]
		}
	}

	// 5) Master gain + safety limiter.
	ml, mr := con.Master.ProcessBlock(n)

	// 6) Tap for the recorder (wait-free pointer load; nil when idle).
	out := e.outScratch[: n*2 : n*2]
	interleave(out, ml, mr, n)
	if rec := e.recorder.Load(); rec != nil {
		rec.Push(out)
	}

	// 7) Metronome click, deliberately after the recorder tap: the click is
	//    a monitoring aid the player hears but a bounce never contains —
	//    standard DAW behavior, and it spares takes from click bleed.
	e.click.Render(out, n)

	// 8) Hand the block to the device.
	copy(outAll[offset*2:(offset+n)*2], out)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
