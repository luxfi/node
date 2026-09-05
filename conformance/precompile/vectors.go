// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Where the borrowed vectors come from. They are the reference
// implementation's own test data, which is the point: an input geth tests
// itself against is an input the other two should survive.
const gethVectors = "core/vm/testdata/precompiles"

// How many cases to take from each borrowed file.
const perFile = 12

// Which address each of those files exercises. A file with no entry here is
// skipped rather than guessed at: `blsG1Mul.json` is the pre-final EIP-2537
// shape with no address of its own after the multiply and multi-exponentiation
// precompiles merged, and putting it at the multi-exponentiation address would
// be inventing a claim.
var fileAddress = map[string]string{
	"ecRecover":       addr(0x01),
	"modexp":          addr(0x05),
	"modexp_eip2565":  addr(0x05),
	"bn256Add":        addr(0x06),
	"bn256ScalarMul":  addr(0x07),
	"bn256Pairing":    addr(0x08),
	"blake2F":         addr(0x09),
	"pointEvaluation": addr(0x0a),
	"blsG1Add":        addr(0x0b),
	"blsG1MultiExp":   addr(0x0c),
	"blsG2Add":        addr(0x0d),
	"blsG2MultiExp":   addr(0x0e),
	"blsPairing":      addr(0x0f),
	"blsMapG1":        addr(0x10),
	"blsMapG2":        addr(0x11),
}

func addr(last ...byte) string {
	b := make([]byte, 20)
	copy(b[20-len(last):], last)
	return hex(b)
}

// borrowed reads geth's precompile test data, if it is where it says it is.
// The corpus is generated, not committed twice: a vector file copied into this
// repo is a vector file that stops tracking the one it was copied from.
type gethCase struct {
	Input    string
	Expected string
	Gas      uint64
	Name     string
}

func borrowed(root string) ([]Vector, error) {
	dir := filepath.Join(root, gethVectors)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("geth vectors: %w (set LUX_GETH to the checkout)", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var out []Vector
	for _, name := range names {
		stem := strings.TrimSuffix(name, ".json")
		lookup := strings.TrimPrefix(stem, "fail-")
		// geth's failing files are named for the precompile in lower camel,
		// the passing ones in the same case they register under.
		address, ok := fileAddress[lookup]
		if !ok {
			for k, v := range fileAddress {
				if strings.EqualFold(k, lookup) {
					address, ok = v, true
					break
				}
			}
		}
		if !ok {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var cases []gethCase
		if err := json.Unmarshal(raw, &cases); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		// Enough of each file to cover its shapes, not all of it. The
		// multi-exponentiation files run to hundreds of near-identical
		// entries of several kilobytes each, and a corpus nobody can read is
		// one nobody checks.
		if len(cases) > perFile {
			cases = cases[:perFile]
		}
		for i, c := range cases {
			in, err := unhex(c.Input)
			if err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", name, i, err)
			}
			// Twice the charge, so a vector that is about the output is not
			// also about the gas limit. The limit is exercised deliberately,
			// below, where that is the whole question.
			limit := c.Gas * 2
			if limit == 0 {
				limit = 1 << 20
			}
			out = append(out, Vector{
				ID:      id(stem, i, c.Name),
				Address: address,
				Gas:     limit,
				Input:   in,
			})
		}
	}
	return out, nil
}

func id(stem string, i int, name string) string {
	s := stem
	if name != "" {
		s += "_" + name
	} else {
		s += fmt.Sprintf("_%d", i)
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, s)
	return strings.ToUpper(s)
}
