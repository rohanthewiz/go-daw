package ui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/rohanthewiz/go-daw/audio/source"
	"github.com/rohanthewiz/logger"
	"github.com/rohanthewiz/serr"
)

// gmPrograms is the standard General MIDI Level 1 program map (0-based). The
// names are part of the GM spec, so they are correct for any GM-compliant
// bank — which is exactly what the soundbanks folder is meant to hold. Banks
// with custom presets still work; the labels are then merely approximate.
var gmPrograms = [128]string{
	// Piano
	"Acoustic Grand Piano", "Bright Acoustic Piano", "Electric Grand Piano", "Honky-tonk Piano",
	"Electric Piano 1", "Electric Piano 2", "Harpsichord", "Clavinet",
	// Chromatic percussion
	"Celesta", "Glockenspiel", "Music Box", "Vibraphone",
	"Marimba", "Xylophone", "Tubular Bells", "Dulcimer",
	// Organ
	"Drawbar Organ", "Percussive Organ", "Rock Organ", "Church Organ",
	"Reed Organ", "Accordion", "Harmonica", "Tango Accordion",
	// Guitar
	"Acoustic Guitar (nylon)", "Acoustic Guitar (steel)", "Electric Guitar (jazz)", "Electric Guitar (clean)",
	"Electric Guitar (muted)", "Overdriven Guitar", "Distortion Guitar", "Guitar Harmonics",
	// Bass
	"Acoustic Bass", "Electric Bass (finger)", "Electric Bass (pick)", "Fretless Bass",
	"Slap Bass 1", "Slap Bass 2", "Synth Bass 1", "Synth Bass 2",
	// Strings
	"Violin", "Viola", "Cello", "Contrabass",
	"Tremolo Strings", "Pizzicato Strings", "Orchestral Harp", "Timpani",
	// Ensemble
	"String Ensemble 1", "String Ensemble 2", "Synth Strings 1", "Synth Strings 2",
	"Choir Aahs", "Voice Oohs", "Synth Voice", "Orchestra Hit",
	// Brass
	"Trumpet", "Trombone", "Tuba", "Muted Trumpet",
	"French Horn", "Brass Section", "Synth Brass 1", "Synth Brass 2",
	// Reed
	"Soprano Sax", "Alto Sax", "Tenor Sax", "Baritone Sax",
	"Oboe", "English Horn", "Bassoon", "Clarinet",
	// Pipe
	"Piccolo", "Flute", "Recorder", "Pan Flute",
	"Blown Bottle", "Shakuhachi", "Whistle", "Ocarina",
	// Synth lead
	"Lead 1 (square)", "Lead 2 (sawtooth)", "Lead 3 (calliope)", "Lead 4 (chiff)",
	"Lead 5 (charang)", "Lead 6 (voice)", "Lead 7 (fifths)", "Lead 8 (bass+lead)",
	// Synth pad
	"Pad 1 (new age)", "Pad 2 (warm)", "Pad 3 (polysynth)", "Pad 4 (choir)",
	"Pad 5 (bowed)", "Pad 6 (metallic)", "Pad 7 (halo)", "Pad 8 (sweep)",
	// Synth effects
	"FX 1 (rain)", "FX 2 (soundtrack)", "FX 3 (crystal)", "FX 4 (atmosphere)",
	"FX 5 (brightness)", "FX 6 (goblins)", "FX 7 (echoes)", "FX 8 (sci-fi)",
	// Ethnic
	"Sitar", "Banjo", "Shamisen", "Koto",
	"Kalimba", "Bagpipe", "Fiddle", "Shanai",
	// Percussive
	"Tinkle Bell", "Agogo", "Steel Drums", "Woodblock",
	"Taiko Drum", "Melodic Tom", "Synth Drum", "Reverse Cymbal",
	// Sound effects
	"Guitar Fret Noise", "Breath Noise", "Seashore", "Bird Tweet",
	"Telephone Ring", "Helicopter", "Applause", "Gunshot",
}

// drumKitOption is one entry in the kit dropdown: the program number sent as a
// channel-9 program change, and the label shown to the player.
type drumKitOption struct {
	Prog int
	Name string
}

// gmDrumKits lists the GS-standard percussion kits by program number on the
// drum channel. A slice of pairs (not a [128]string) because kit numbers are
// sparse — only these nine are conventional, and listing 119 empty slots in a
// dropdown would bury the real choices. Banks that lack a kit fall back to
// the default (Standard) preset in the synth, so every entry is safe to offer.
//
// This is now only the fallback for when a bank's own preset list is
// unreadable; see drumKitOptions.
var gmDrumKits = []drumKitOption{
	{0, "Standard"},
	{8, "Room"},
	{16, "Power"},
	{24, "Electronic"},
	{25, "TR-808"},
	{32, "Jazz"},
	{40, "Brush"},
	{48, "Orchestra"},
	{56, "SFX"},
}

// drumKitOptions builds the kit dropdown for the bank at bankPath, preferring
// the bank's own bank-128 preset names over the fixed GS table. Real banks
// diverge from the table in both directions — GeneralUser-GS calls kit 25
// "808/909" and adds a "CM-64/32L" at 127, while MuseScore_General exposes
// seven Standard variants and a "Marching Snare" — so listing what the bank
// actually contains is both more accurate and more discoverable.
//
// Falls back to gmDrumKits when the bank can't be read or declares no
// percussion presets, because an empty dropdown would strand a player in drums
// mode with no way to change kits; the GS entries still work there, since a
// missing preset degrades to the synth's default rather than going silent.
//
// cur is the kit currently selected on the channel. If the bank doesn't define
// it (common right after a bank switch: the old bank's kit 26 has no counterpart
// in the new one), a placeholder entry is inserted so the select keeps showing
// the server's actual state instead of silently snapping to the first option and
// lying about what is playing.
func drumKitOptions(bankPath string, cur int) []drumKitOption {
	kits := gmDrumKits
	if bankPath != "" {
		if presets, err := source.BankDrumKits(bankPath); err == nil && len(presets) > 0 {
			kits = make([]drumKitOption, 0, len(presets))
			for _, p := range presets {
				kits = append(kits, drumKitOption{Prog: p.Program, Name: p.Name})
			}
		} else if err != nil {
			logger.LogErr(serr.Wrap(err, "bank", bankPath), "msg", "reading bank drum kits; falling back to the GS table")
		}
	}

	for _, k := range kits {
		if k.Prog == cur {
			return kits
		}
	}
	// Insert in program order so the placeholder lands where the player expects
	// that number to be, rather than jumping to the top of the list.
	miss := drumKitOption{Prog: cur, Name: "(not in bank — Standard)"}
	at := sort.Search(len(kits), func(i int) bool { return kits[i].Prog >= cur })
	out := make([]drumKitOption, 0, len(kits)+1)
	out = append(out, kits[:at]...)
	out = append(out, miss)
	return append(out, kits[at:]...)
}

// sfontBaseName trims a bank path to a compact dropdown label:
// "soundbanks/GeneralUser-GS.sf2" → "GeneralUser-GS".
func sfontBaseName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
