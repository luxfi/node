// SPDX-License-Identifier: BSD-3-Clause-Eco

// The Go side of the cross-language precompile differential: it writes the
// corpus, and it is one of the three evaluators that read it.
//
// Two verbs, the same two the chain generator has:
//
//	precompile emit <dir>       build the corpus and write
//	                            <dir>/precompile_vectors.tsv and
//	                            <dir>/precompile_expected.tsv
//	precompile eval <vectors>   read a corpus and print this
//	                            implementation's verdicts
//
// `emit` writes the expectations by running `eval` over the vectors it just
// built, so they are what Go said when it read them back rather than a second
// opinion typed beside the bytes.
//
// This module depends on luxfi/geth and luxfi/precompile, and nothing else in
// node2 does. It is a separate Go module for that reason, the same way the
// chain reference is.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "emit":
		dir := "../corpus"
		if len(os.Args) > 2 {
			dir = os.Args[2]
		}
		if err := emit(dir); err != nil {
			fmt.Fprintln(os.Stderr, "emit:", err)
			os.Exit(1)
		}
	case "eval":
		path := "../corpus/precompile_vectors.tsv"
		if len(os.Args) > 2 {
			path = os.Args[2]
		}
		if err := eval(path, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "eval:", err)
			os.Exit(1)
		}
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: precompile emit <corpus-dir> | precompile eval <vectors.tsv>")
	os.Exit(2)
}
