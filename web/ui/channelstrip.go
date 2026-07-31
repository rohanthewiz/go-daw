package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rohanthewiz/element"
	"github.com/rohanthewiz/go-daw/audio/source"
	"github.com/rohanthewiz/go-daw/mixer"
	"github.com/rohanthewiz/go-daw/module"
)

// ChannelStrip renders one input channel. Reading the mixer objects directly
// (rather than mapping through a view-model layer) is safe because Render
// runs on the control plane and every read goes through the same atomic
// cells the HTTP handlers use.
type ChannelStrip struct {
	Ch          *mixer.Channel
	GroupCount  int
	ModuleNames []string
	Duplex      bool
	Soundbanks  []string // .sf2 paths for the sfont source selector
}

// Render satisfies element.Component.
func (cs ChannelStrip) Render(b *element.Builder) (x any) {
	ch := cs.Ch
	id := ch.ID

	b.DivClass("strip", "data-strip", strconv.Itoa(id)).R(
		b.DivClass("strip-name").T(ch.Name()),

		cs.renderSource(b),

		// Processing sections live in <details> so a strip stays scannable;
		// the browser remembers open state per element while the page lives.
		b.DetailsClass("sect").R(
			b.Summary().T("Gate"),
			toggle(b, "ch", id, "gate.enabled", "On", ch.Gate.Enabled.Load()),
			slider(b, "ch", id, "gate.threshold", "Thresh", -90, 0, 1, ch.Gate.OpenThreshDB.Get(), ""),
			slider(b, "ch", id, "gate.hysteresis", "Hyst", 0, 24, 1, ch.Gate.HysteresisDB.Get(), ""),
			slider(b, "ch", id, "gate.attack", "Atk", 0.1, 100, 0.1, ch.Gate.AttackMs.Get(), "log"),
			slider(b, "ch", id, "gate.hold", "Hold", 0, 1000, 10, ch.Gate.HoldMs.Get(), ""),
			slider(b, "ch", id, "gate.release", "Rel", 1, 2000, 1, ch.Gate.ReleaseMs.Get(), "log"),
		),

		b.DetailsClass("sect").R(
			b.Summary().T("Filters"),
			toggle(b, "ch", id, "hpf.enabled", "HPF", ch.HPF.Enabled.Load()),
			slider(b, "ch", id, "hpf.freq", "HP Frq", 20, 2000, 1, ch.HPF.Freq.Get(), "log"),
			toggle(b, "ch", id, "lpf.enabled", "LPF", ch.LPF.Enabled.Load()),
			slider(b, "ch", id, "lpf.freq", "LP Frq", 200, 20000, 1, ch.LPF.Freq.Get(), "log"),
		),

		b.DetailsClass("sect", "open", "open").R(
			b.Summary().T("EQ"),
			b.Wrap(func() {
				for band := 0; band < 3; band++ {
					eq := ch.EQ[band]
					p := "eq" + strconv.Itoa(band+1)
					b.DivClass("eq-band").R(
						b.DivClass("eq-band-head").T([]string{"Low", "Mid", "High"}[band]),
						slider(b, "ch", id, p+".freq", "Frq", 20, 20000, 1, eq.Freq.Get(), "log"),
						slider(b, "ch", id, p+".gain", "Gain", -18, 18, 0.5, eq.GainDB.Get(), ""),
						slider(b, "ch", id, p+".q", "Q", 0.1, 10, 0.1, eq.Q.Get(), "log"),
					)
				}
			}),
		),

		b.DetailsClass("sect").R(
			b.Summary().T("Comp"),
			toggle(b, "ch", id, "comp.enabled", "On", ch.Comp.Enabled.Load()),
			slider(b, "ch", id, "comp.threshold", "Thresh", -60, 0, 1, ch.Comp.ThresholdDB.Get(), ""),
			slider(b, "ch", id, "comp.ratio", "Ratio", 1, 20, 0.5, ch.Comp.Ratio.Get(), ""),
			slider(b, "ch", id, "comp.knee", "Knee", 0, 24, 1, ch.Comp.KneeDB.Get(), ""),
			slider(b, "ch", id, "comp.attack", "Atk", 0.1, 200, 0.1, ch.Comp.AttackMs.Get(), "log"),
			slider(b, "ch", id, "comp.release", "Rel", 5, 2000, 5, ch.Comp.ReleaseMs.Get(), "log"),
			slider(b, "ch", id, "comp.makeup", "Makeup", 0, 24, 0.5, ch.Comp.MakeupDB.Get(), ""),
		),

		b.DetailsClass("sect").R(
			b.Summary().T("Reverb"),
			toggle(b, "ch", id, "reverb.enabled", "On", ch.Reverb.Enabled.Load()),
			slider(b, "ch", id, "reverb.predelay", "PreDly", 0, 250, 1, ch.Reverb.PreDelayMs.Get(), ""),
			slider(b, "ch", id, "reverb.decay", "Decay", 0, 1, 0.01, ch.Reverb.Decay.Get(), ""),
			slider(b, "ch", id, "reverb.damp", "Damp", 0, 1, 0.01, ch.Reverb.Damp.Get(), ""),
			slider(b, "ch", id, "reverb.mix", "Mix", 0, 1, 0.01, ch.Reverb.Mix.Get(), ""),
		),

		cs.renderModules(b),

		// Bottom of strip: pan, routing, mute, then fader+meter.
		slider(b, "ch", id, "pan", "Pan", -1, 1, 0.01, ch.Pan.Get(), ""),

		b.DivClass("route-row").R(
			b.Label().T("Bus"),
			b.Select("data-role", "group-select", "data-id", strconv.Itoa(id)).R(
				b.Wrap(func() {
					cur := int(ch.GroupIdx.Load())
					opt := func(val int, label string) {
						attrs := []string{"value", strconv.Itoa(val)}
						if val == cur {
							attrs = append(attrs, "selected", "selected")
						}
						b.Option(attrs...).T(label)
					}
					opt(0, "Master")
					for g := 1; g <= cs.GroupCount; g++ {
						opt(g, "Grp "+strconv.Itoa(g))
					}
				}),
			),
		),

		b.Wrap(func() {
			attrs := []string{"data-target", "ch", "data-id", strconv.Itoa(id), "data-param", "mute"}
			if ch.Mute.Load() {
				attrs = append(attrs, "data-on", "1")
			}
			b.ButtonClass("mute-btn", attrs...).T("MUTE")
		}),

		b.DivClass("fader-row").R(
			b.DivClass("fader-wrap").R(
				b.InputClass("fader", "type", "range",
					"data-target", "ch", "data-id", strconv.Itoa(id), "data-param", "gain",
					"min", "-60", "max", "12", "step", "0.1",
					"value", fmt.Sprintf("%.1f", ch.GainDB.Get()),
				).R(),
				b.SpanClass("readout fader-readout").F("%.1f", ch.GainDB.Get()),
			),
			meterHTML(b, "ch-"+strconv.Itoa(id)),
		),
	)
	return
}

// renderSource draws the source selector + oscillator/wav sub-controls.
func (cs ChannelStrip) renderSource(b *element.Builder) (x any) {
	ch := cs.Ch
	id := strconv.Itoa(ch.ID)

	srcType := "none"
	oscFreq, oscLevel := 220.0, -18.0
	synthLevel := -12.0
	sfontLevel, sfontPath, sfontProg, sfontDrums := -6.0, "", 0, false
	wavPath := ""
	switch s := ch.Source().(type) {
	case *source.Oscillator:
		srcType = "osc"
		oscFreq = s.FreqHz.Get()
		oscLevel = s.LevelDB.Get()
	case *source.PolySynth:
		srcType = "synth"
		synthLevel = s.LevelDB.Get()
	case *source.SFSynth:
		srcType = "sfont"
		sfontLevel = s.LevelDB.Get()
		sfontPath = s.Path()
		sfontProg = s.Program()
		sfontDrums = s.Drums()
	case *source.WavSource:
		srcType = "wav"
		wavPath = strings.TrimPrefix(s.Name(), "wav:")
	case *source.LiveSource:
		srcType = "live"
	}

	b.DivClass("src-row").R(
		b.Label().T("Src"),
		b.Select("data-role", "source-select", "data-id", id).R(
			b.Wrap(func() {
				for _, opt := range []string{"none", "osc", "synth", "sfont", "live", "wav"} {
					attrs := []string{"value", opt}
					if opt == srcType {
						attrs = append(attrs, "selected", "selected")
					}
					if opt == "live" && !cs.Duplex {
						attrs = append(attrs, "disabled", "disabled")
					}
					// No banks on disk means sfont can't be built; disabling
					// (rather than hiding) advertises the feature and hints at
					// the fix — drop an .sf2 into the soundbanks folder.
					if opt == "sfont" && len(cs.Soundbanks) == 0 {
						attrs = append(attrs, "disabled", "disabled")
					}
					b.Option(attrs...).T(opt)
				}
			}),
		),
		// Sub-controls; JS shows the row matching the selected type.
		b.DivClass("src-osc", "data-id", id).R(
			slider(b, "src", ch.ID, "osc.freq", "Freq", 20, 5000, 1, oscFreq, "log"),
			slider(b, "src", ch.ID, "osc.level", "Lvl", -60, 0, 1, oscLevel, ""),
		),
		b.DivClass("src-synth", "data-id", id).R(
			slider(b, "src", ch.ID, "synth.level", "Lvl", -60, 0, 1, synthLevel, ""),
		),
		// SoundFont sub-controls: bank + instrument selects and a level trim.
		// Bank changes rebuild the source (new sample set → new synthesizer);
		// program changes are live events into the running synth, so the two
		// selects deliberately carry different data-roles.
		b.DivClass("src-sfont", "data-id", id).R(
			b.Select("data-role", "sfont-bank", "data-id", id, "title", "SoundFont bank").R(
				b.Wrap(func() {
					for _, bank := range cs.Soundbanks {
						attrs := []string{"value", bank}
						if bank == sfontPath {
							attrs = append(attrs, "selected", "selected")
						}
						b.Option(attrs...).T(sfontBaseName(bank))
					}
				}),
			),
			b.Select("data-role", "sfont-program", "data-id", id, "title", "GM instrument").R(
				b.Wrap(func() {
					for p, name := range gmPrograms {
						attrs := []string{"value", strconv.Itoa(p)}
						if p == sfontProg {
							attrs = append(attrs, "selected", "selected")
						}
						b.Option(attrs...).T(strconv.Itoa(p) + " · " + name)
					}
				}),
			),
			slider(b, "src", ch.ID, "sfont.level", "Lvl", -60, 0, 1, sfontLevel, ""),
			// Drums reroutes notes to the GM percussion channel — a live
			// source-param like program, so no rebuild/reload on toggle.
			toggle(b, "src", ch.ID, "sfont.drums", "Drums", sfontDrums),
		),
		b.DivClass("src-wav", "data-id", id).R(
			b.Input("type", "text", "placeholder", "path/to/file.wav",
				"data-role", "wav-path", "data-id", id, "value", wavPath).R(),
			b.Button("data-role", "wav-load", "data-id", id).T("Load"),
		),
	)
	return
}

// renderModules draws the insert chain with auto-generated param controls
// from each module's ParamSpec metadata, plus the add-module dropdown.
func (cs ChannelStrip) renderModules(b *element.Builder) (x any) {
	ch := cs.Ch
	id := strconv.Itoa(ch.ID)

	b.DetailsClass("sect", "open", "open").R(
		b.Summary().T("Modules"),
		b.Wrap(func() {
			for idx, m := range ch.Modules() {
				idxStr := strconv.Itoa(idx)
				b.DivClass("mod").R(
					b.DivClass("mod-head").R(
						b.Span().T(m.Name()),
						b.ButtonClass("mod-remove", "data-role", "mod-remove",
							"data-id", id, "data-idx", idxStr).T("×"),
					),
					b.Wrap(func() {
						for _, spec := range m.Params() {
							scale := ""
							if spec.Scale == module.ScaleLog {
								scale = "log"
							}
							modSlider(b, ch.ID, idx, spec, m.GetParam(spec.ID), scale)
						}
					}),
				)
			}
		}),
		b.DivClass("mod-add").R(
			b.Select("data-role", "mod-select", "data-id", id).R(
				b.Wrap(func() {
					b.Option("value", "").T("+ module…")
					for _, name := range cs.ModuleNames {
						b.Option("value", name).T(name)
					}
				}),
			),
		),
	)
	return
}

// modSlider is like slider but addresses a module instance parameter.
func modSlider(b *element.Builder, chID, modIdx int, spec module.ParamSpec, value float64, scale string) (x any) {
	attrs := []string{
		"type", "range",
		"data-target", "mod",
		"data-id", strconv.Itoa(chID),
		"data-idx", strconv.Itoa(modIdx),
		"data-param", spec.ID,
	}
	if scale == "log" {
		attrs = append(attrs,
			"min", "0", "max", "1000", "step", "1",
			"value", strconv.Itoa(logPos(value, spec.Min, spec.Max)),
			"data-log", "1", "data-min", fmtF(spec.Min), "data-max", fmtF(spec.Max),
		)
	} else {
		step := (spec.Max - spec.Min) / 100
		attrs = append(attrs,
			"min", fmtF(spec.Min), "max", fmtF(spec.Max), "step", fmtF(step),
			"value", fmtF(value),
		)
	}
	b.DivClass("ctl").R(
		b.Label().T(spec.Name),
		b.Input(attrs...).R(),
		b.SpanClass("readout").T(fmt.Sprintf("%.1f", value)),
	)
	return
}

// meterHTML renders the vertical meter track for a bus key ("ch-3", "grp-1",
// "master"): an RMS fill bar plus a peak tick, both driven by SSE.
func meterHTML(b *element.Builder, key string) (x any) {
	b.DivClass("meter", "data-meter", key).R(
		b.DivClass("meter-rms").R(),
		b.DivClass("meter-peak").R(),
	)
	return
}
