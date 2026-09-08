# The precompile differential

One corpus of calls — an address, a gas limit, and input bytes — handed to
three implementations of the EVM precompiles, whose answers are compared field
by field. It is the same machine as the chain differential next to it, with a
different subject and the same runner.

    make precompiles

## Why a differential and not a test suite

Each runtime already tests its own precompiles, and each of them passes. That
is the problem: a suite written against an implementation agrees with the
implementation it was written against. The question here is not whether
ecrecover works, it is whether the three runtimes charge the same price and
return the same bytes for the same call — and no suite that lives inside one
of them can ask that.

## The corpus

`corpus/precompile_vectors.tsv`, one call per line:

    V	<id>	<address>	<gas-limit>	<input-hex>

The address is twenty bytes of lower-case hex with no leading `0x`. Input is
lower-case hex, or `-` for no bytes. Most of the corpus is borrowed from
geth's own `core/vm/testdata/precompiles`: an input the reference tests itself
against is an input the other two should survive. It is read out of the geth
the Go evaluator's module requires, located with `go list -m`, so the corpus
and the reference cannot be two different versions of geth — they were, and
the corpus was two patch releases behind the requirement with nothing to say
so.

The rest covers what that data does not reach — the three precompiles it ships
no vectors for, an empty input to every address, addresses nobody serves, the
boundary between paying and not paying, and Lux's own precompiles, which geth
has no data for because they are not geth's: inference at `0x0300…0003`, AI
mining at `0x0300…0000`, p256 at `0x…0100`, and the post-quantum block of
LP-4200 — ML-KEM `0x…012201`, ML-DSA `0x…012202`, SLH-DSA `0x…012203`, Pulsar
`0x…012204`, P3Q `0x…012205`, Corona `0x…012206` and X-Wing `0x…2221`. An
empty input is a defined call to every one of them, so what those rows settle
is not how each parses its arguments; it is which implementations are at the
address at all.

## The answer

Each implementation prints one line per vector:

    R	<id>	<status>	<gas-charged>	<output-hex>	<note>

Status is one of:

| | |
|---|---|
| `OK` | it ran; the output is the bytes it returned |
| `FAILED` | it read the input and refused it |
| `OOG` | the gas offered was below what it charges |
| `ABSENT` | nothing serves this address here |
| `STATE` | something serves it, and answering needs a chain |

**Gas is what was charged, never what was left.** The three runtimes disagree
about which of those they return — Go hands back remaining gas, the C++ result
carries gas left and zeroes it on failure, the Rust result carries a revm
`Gas` — and a wire format that let each send its own convention would compare
two different quantities and call the difference a bug. Convert at the edge.

On `OOG` and `ABSENT` the charge is `0` and the output is `-`, because there is
no charge to report and a number nobody agrees to means the column stops
comparing. On `FAILED` the charge stands: a precompile that read the input and
refused it did the work of reading it.

An implementation that cannot know the charge writes `SKIPPED` rather than a
number. revm computes a precompile's price inside the function that does its
work, so a call that returned an error recorded no charge and none can be
recovered; Go and the C++ tree separate price from work and do report it.
Zero would be an answer and a wrong one, since the call was not free. The
runner counts a `SKIPPED` field, prints it under NOT COMPARED, and never
scores it as agreement.

The note is not compared. An error string is a fact about a codebase, not
about a precompile, so it travels where it explains without being weighed.

## Which implementation answers at an address

A Lux module registered at an address **shadows** the stock precompile there,
because that is what `LuxPrecompileOverrider` does before the standard table is
consulted. The Go reference asks in that order. Asking the stock table first
would report a price no chain charges — which matters at `0x…0100`, where the
stock table charges 6900 and the Lux module charges 3450.

## Adding an implementation

Write a program that takes the corpus path as its one argument and prints the
result lines above to standard output. It does not need to answer every
address — but it must print a row for every vector, `ABSENT` included, because
the runner fails a run where an implementation stayed silent. Then add it to
the `precompiles` target as another `-eval`.
