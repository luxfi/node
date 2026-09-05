// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// A vector is one call: an address, what the caller was willing to pay, and
// the bytes. Nothing about what should happen — that is what the three
// implementations are for.
type Vector struct {
	ID      string
	Address string // 20 bytes, lower-case hex, no leading 0x
	Gas     uint64
	Input   []byte
}

func (v Vector) Line() string {
	in := "-"
	if len(v.Input) > 0 {
		in = hex(v.Input)
	}
	return fmt.Sprintf("V\t%s\t%s\t%d\t%s", v.ID, v.Address, v.Gas, in)
}

// The four things an implementation can say about a call.
//
// They are deliberately coarse. Two implementations that both refuse an input
// agree, whatever their messages say, and the message travels in the note
// where it explains without being compared — an error string is a fact about a
// codebase, not about a precompile.
const (
	OK      = "OK"     // it ran; output is the bytes it returned
	FAILED  = "FAILED" // it read the input and refused it
	OOG     = "OOG"    // the gas offered was below what it charges
	ABSENT  = "ABSENT" // nothing serves this address here
	STATE   = "STATE"  // something serves it, and answering needs a chain
	noBytes = "-"      // an empty output, spelled so it is not an empty column
)

// A Result answers a vector. Gas is what was CHARGED, never what was left:
// the three implementations disagree about which of those they return, and a
// wire format that let each of them send its own convention would compare two
// different quantities and call the difference a bug.
//
// On OOG and ABSENT the charge is 0 by convention, because there is no charge
// to report and a number nobody agrees to means the column stops comparing.
type Result struct {
	ID     string
	Status string
	Gas    uint64
	Output []byte
	Note   string
}

func (r Result) Line() string {
	out := noBytes
	if len(r.Output) > 0 {
		out = hex(r.Output)
	}
	return fmt.Sprintf("R\t%s\t%s\t%d\t%s\t%s", r.ID, r.Status, r.Gas, out, r.Note)
}

func hex(b []byte) string {
	const digits = "0123456789abcdef"
	s := make([]byte, 0, len(b)*2)
	for _, c := range b {
		s = append(s, digits[c>>4], digits[c&0x0f])
	}
	return string(s)
}

func unhex(s string) ([]byte, error) {
	if s == noBytes {
		return nil, nil
	}
	s = strings.TrimPrefix(s, "0x")
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd-length hex: %d chars", len(s))
	}
	b := make([]byte, len(s)/2)
	for i := range b {
		n, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, err
		}
		b[i] = byte(n)
	}
	return b, nil
}

// ParseVector reads one V line. The columns are id, address, gas, input.
func ParseVector(line string) (Vector, error) {
	f := strings.Split(line, "\t")
	if len(f) != 5 || f[0] != "V" {
		return Vector{}, fmt.Errorf("not a vector line: %q", line)
	}
	gas, err := strconv.ParseUint(f[3], 10, 64)
	if err != nil {
		return Vector{}, fmt.Errorf("%s: gas: %w", f[1], err)
	}
	in, err := unhex(f[4])
	if err != nil {
		return Vector{}, fmt.Errorf("%s: input: %w", f[1], err)
	}
	return Vector{ID: f[1], Address: strings.ToLower(f[2]), Gas: gas, Input: in}, nil
}
