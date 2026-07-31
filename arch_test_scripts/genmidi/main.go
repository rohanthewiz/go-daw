// genmidi writes a small format-0 demo song (8 bars, C-G-Am-F, piano + bass +
// drums at 120 BPM) for the go-daw midifiles folder.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"
)

const tpq = 480 // ticks per quarter
const bar = 4 * tpq

type ev struct {
	tick int
	data []byte
}

var events []ev

func at(tick int, data ...byte) { events = append(events, ev{tick, data}) }

func note(ch, n, vel, tick, dur int) {
	at(tick, byte(0x90|ch), byte(n), byte(vel))
	at(tick+dur, byte(0x80|ch), byte(n), 0)
}

func varint(v int) []byte {
	if v < 0 {
		panic("negative delta")
	}
	out := []byte{byte(v & 0x7F)}
	for v >>= 7; v > 0; v >>= 7 {
		out = append([]byte{byte(v&0x7F | 0x80)}, out...)
	}
	return out
}

func main() {
	// Programs: ch0 piano (0), ch1 fingered bass (33), ch2 strings (48)
	at(0, 0xC0, 0)
	at(0, 0xC1, 33)
	at(0, 0xC2, 48)

	chords := [][]int{ // C, G, Am, F — repeated twice
		{60, 64, 67}, {59, 62, 67}, {57, 60, 64}, {57, 60, 65},
		{60, 64, 67}, {59, 62, 67}, {57, 60, 64}, {57, 60, 65},
	}
	roots := []int{36, 43, 45, 41, 36, 43, 45, 41}
	melody := [][]int{ // one quarter-note per beat, chord tones an octave up
		{72, 76, 79, 76}, {74, 79, 74, 71}, {72, 76, 72, 69}, {69, 72, 77, 72},
		{72, 76, 79, 76}, {74, 79, 74, 71}, {72, 76, 72, 69}, {77, 76, 74, 72},
	}

	for b := 0; b < 8; b++ {
		t0 := b * bar
		for _, n := range chords[b] { // piano: whole-note chord
			note(0, n, 72, t0, bar-40)
		}
		for q := 0; q < 4; q++ { // bass: root quarters
			note(1, roots[b], 88, t0+q*tpq, tpq-40)
			note(2, melody[b][q], 60, t0+q*tpq, tpq-20) // strings melody
		}
		for e := 0; e < 8; e++ { // drums: hats eighths
			note(9, 42, 60, t0+e*tpq/2, 60)
		}
		note(9, 36, 100, t0, 60)         // kick beat 1
		note(9, 36, 90, t0+2*tpq, 60)    // kick beat 3
		note(9, 38, 90, t0+tpq, 60)      // snare beat 2
		note(9, 38, 95, t0+3*tpq, 60)    // snare beat 4
	}

	sort.SliceStable(events, func(i, j int) bool { return events[i].tick < events[j].tick })

	var track []byte
	// Explicit tempo: 120 BPM = 500000 µs/quarter
	track = append(track, 0x00, 0xFF, 0x51, 0x03, 0x07, 0xA1, 0x20)
	last := 0
	for _, e := range events {
		track = append(track, varint(e.tick-last)...)
		track = append(track, e.data...)
		last = e.tick
	}
	track = append(track, varint(bar/2)...) // half-bar of silence before EOT
	track = append(track, 0xFF, 0x2F, 0x00)

	var out []byte
	out = append(out, 'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 0, 0, 1)
	out = binary.BigEndian.AppendUint16(out, tpq)
	out = append(out, 'M', 'T', 'r', 'k')
	out = binary.BigEndian.AppendUint32(out, uint32(len(track)))
	out = append(out, track...)

	if err := os.WriteFile("demo.mid", out, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("wrote demo.mid: %d bytes, %d events\n", len(out), len(events))
}
