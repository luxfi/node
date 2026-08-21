// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCanonicalHost_IsIdempotent is the property that makes a canonical form
// canonical: running it twice must change nothing. Two nodes that normalise a
// different number of times have to reach the same string, or a hostname claim
// signed by one fails to match the same claim seen by the other.
func TestCanonicalHost_IsIdempotent(t *testing.T) {
	for _, in := range []string{
		"node.lux.network",
		"  Node.LUX.Network.  ",
		"a-b-c.d-e-f.example",
		"XN--80AK6AA92E.com",
		"1.2.3.4.5",
		strings.Repeat("a", 63) + ".example",
	} {
		once, err := CanonicalHost(in)
		require.NoError(t, err, "input=%q", in)
		twice, err := CanonicalHost(once)
		require.NoError(t, err, "input=%q", in)
		require.Equal(t, once, twice, "input=%q: canonicalisation must be a fixed point", in)
	}
}

// TestCanonicalHost_CaseAndPresentationCollapse pins what is presentation and
// what is identity. Case, surrounding whitespace, the FQDN trailing dot and
// IPv6 brackets are all ways of writing the same name; anything that survives
// them is the name itself.
func TestCanonicalHost_CaseAndPresentationCollapse(t *testing.T) {
	same := []string{
		"node.lux.network",
		"NODE.LUX.NETWORK",
		"Node.Lux.Network",
		"node.lux.network.",
		"  node.lux.network  ",
		"\tNODE.lux.Network.\n",
	}
	want, err := CanonicalHost(same[0])
	require.NoError(t, err)
	require.Equal(t, "node.lux.network", want)

	for _, in := range same[1:] {
		got, err := CanonicalHost(in)
		require.NoError(t, err, "input=%q", in)
		require.Equal(t, want, got, "input=%q must normalise to the same name", in)
	}
}

// TestCanonicalHost_RefusesIPLiterals is the one refusal with teeth. A
// hostname endpoint exists so a node behind a proxy can be reached by name; an
// address wearing a name's clothes gets two code paths to disagree about the
// same peer, in both notations and with or without brackets.
func TestCanonicalHost_RefusesIPLiterals(t *testing.T) {
	for _, in := range []string{
		"127.0.0.1",
		"10.0.0.1",
		"0.0.0.0",
		"255.255.255.255",
		"::1",
		"[::1]",
		"fe80::1",
		"[fe80::1]",
		"2001:db8::dead:beef",
		"::ffff:127.0.0.1",
		"[::ffff:127.0.0.1]",
	} {
		got, err := CanonicalHost(in)
		require.ErrorIs(t, err, ErrIPLiteralNotAllowed, "input=%q", in)
		require.Empty(t, got, "input=%q", in)
	}
}

// TestCanonicalHost_RefusesMalformedNames covers the shapes a resolver, a
// certificate matcher and a log line each read differently. Empty labels are
// the sharp one: "a..b" is not "a.b" anywhere, and letting it through means
// two spellings of one claim.
func TestCanonicalHost_RefusesMalformedNames(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		".",
		"..",
		"a..b",
		".leading",
		"trailing..",
		"-leading.example",
		"trailing-.example",
		"exa mple.com",
		"under_score.example",
		"semi;colon.example",
		"slash/es.example",
		"null\x00byte.example",
		"emoji☃.example",
		"at@sign.example",
		"colon:1234",
		strings.Repeat("a", 64) + ".example", // one over the 63-byte label limit
		"[unclosed.example",
	} {
		got, err := CanonicalHost(in)
		require.Error(t, err, "input=%q must be refused", in)
		require.Empty(t, got, "input=%q", in)
	}
}

// TestCanonicalHost_AcceptsWhatDNSAccepts is the other half: the refusals must
// not be so eager that a legal name is rejected. A hyphen inside a label, a
// digit-leading label and a punycode label are all ordinary DNS.
func TestCanonicalHost_AcceptsWhatDNSAccepts(t *testing.T) {
	for in, want := range map[string]string{
		"node":                          "node",
		"node.lux.network":              "node.lux.network",
		"a-b.example":                   "a-b.example",
		"9node.example":                 "9node.example",
		"xn--80ak6aa92e.com":            "xn--80ak6aa92e.com",
		"validator-07.eu-west.lux.cloud": "validator-07.eu-west.lux.cloud",
		strings.Repeat("a", 63):         strings.Repeat("a", 63), // exactly at the label limit
	} {
		got, err := CanonicalHost(in)
		require.NoError(t, err, "input=%q", in)
		require.Equal(t, want, got, "input=%q", in)
	}
}

// TestCanonicalHost_BracketsOnlyStripWhenTheyMatch keeps the IPv6 bracket
// stripping from eating a bracket that is part of an (illegal) name: an
// unbalanced bracket must be refused as a bad character, not quietly trimmed.
func TestCanonicalHost_BracketsOnlyStripWhenTheyMatch(t *testing.T) {
	_, err := CanonicalHost("[node.example]")
	require.NoError(t, err, "a matched pair is stripped and the name inside stands")

	got, err := CanonicalHost("[node.example]")
	require.NoError(t, err)
	require.Equal(t, "node.example", got)

	for _, in := range []string{"[node.example", "node.example]", "[]"} {
		_, err := CanonicalHost(in)
		require.ErrorIs(t, err, ErrInvalidHost, "input=%q", in)
	}
}
