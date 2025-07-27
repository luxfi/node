# Lux Consensus

Welcome to **Lux Consensus**, a photonics-inspired, leaderless BFT protocol that
fuses elegant physics metaphors with cutting‑edge probabilistic metastable
consensus with:

* **Sub‑second finality**: Rapid β‑round focus sharpens decisions in mere hundreds of milliseconds.
* **Ultra‑high throughput**: Scales to tens of thousands of TPS on 10 Gbps networks by sampling only K peers per round.
* **Tunable safety**: Configurable thresholds (αₚ, α𝚌, β) let you drive ε‑safety down to 10⁻⁹ even under 20% Byzantine faults.
* **Lean, modular design**: Swap or benchmark any stage—Photon (sampling), Wave (quorum), Focus (confirmation), Beam (linear), Nova (DAG)—independently.
* **Graceful degradation**: Safety degrades smoothly beyond thresholds, making parameter selection intuitive.

Dive in below to explore how light‑based metaphors illuminate the path to scalable, green, leaderless consensus.

## 🌟 Overview

Lux Consensus uses a five stage process to reach consensus:

|  Stage           | Description                                                      | Objective    |
|  --------------- | ---------------------------------------------------------------- | ------------ |
|  1. **Photon**      | Emit and detect “photons” (queries) to sample validator opinions | Poll         |
|  2. **Wave**        | Wave interference (vote counting) to detect quorum               | Threshold    |
|  3. **Focus**       | Focus β rounds to build confidence                               | Confirmation |
|  4. **Beam**        | Linear-chain consensus forming a coherent light beam             | Chain Engine |
|  5. **Nova**        | DAG-based consensus spreading like a nova explosion              | DAG Engine   |

## 📦 Package Structure

```text
photon/     # Sampling (Photon)             factories and samplers
threshold/  # Quorum (Wave)                static & dynamic α thresholds
focus/      # Confidence (Focus)            β-round tracking
engines/
  beam/       # Linear Consensus (Beam)        chain engine & block ordering
  nova/       # DAG Consensus (Nova)          DAG engine & vertex ordering
config/     # Parameter builders & validators
networking/ # P2P, routing, metrics
choices/    # Decidable interfaces & mocks
testing/    # Simulators, mocks & fuzzers
```
*Each package is self-contained to avoid cross-dependencies.*

## 🔬 How Lux Consensus Works

### 1. Photon (Sampling)

```go
sampler := photon.NewFactory(k).NewBinary()
sample, _ := sampler.Sample(ctx, validators, k)
```

### 2. Wave (Thresholding)

```go
threshold := threshold.NewFactory(alphaPref, alphaConf).NewDynamic()
if threshold.Add(vote) {
    // Quorum reached via wave interference
}
```

### 3. Focus (β-Round Confirmation)

```go
conf := focus.NewFactory(beta).NewBinary()
conf.Record(success, choice)
if conf.IsFocused() {
    // Consensus focus achieved
}
```

### 4a. Beam (Linear Engine)

```go
engine := beam.NewEngine(params)
engine.Add(ctx, block)
engine.RecordPoll(ctx, votes)
if engine.Finalized() { … }
```

### 4b. Nova (DAG Engine)

```go
engine := nova.NewEngine(params)
engine.Add(ctx, vertex)
engine.RecordPoll(ctx, votes)
if engine.Preferred(vertex) { … }
```

## 🎯 Photonic Parameters

* **K**               Sample size (photons per round)
* **AlphaPreference** Wave threshold for initial preference
* **AlphaConfidence** Wave threshold for confidence votes
* **Beta**            Number of Focus rounds for finality

## 📊 Performance

Measured on a 10 Gbps LAN (batch=40):

| Network     | Nodes | TPS          | Median Latency | Max Latency | Safety ε         |
| ----------- | ----- | ------------ | -------------- | ----------- | ---------------- |
| **Mainnet** | 21    | \~7 000 tps  | \~0.30 s       | \~0.40 s    | ε≤10⁻⁹ @20% f    |
| **Testnet** | 11    | \~3 000 tps  | \~0.60 s       | \~0.80 s    | ε≤10⁻⁹ @13–16% f |
| **Local**   | 5     | \~20 000 tps | \~0.06 s       | \~0.10 s    | ε≤10⁻⁹ @30% f    |

## 🚀 Usage Example

```go
params := beam.Parameters{K:21, AlphaPreference:13, AlphaConfidence:18, Beta:8}
beamEng := beam.NewEngine(params)

beamEng.Add(ctx, block)
beamEng.RecordPoll(ctx, votes)
if blocks := beamEng.Finalized(); len(blocks)>0 {
  fmt.Println("Blocks finalized:", blocks)
}

novaParams := nova.Parameters{K:21, AlphaPref:13, AlphaConf:18, Beta:8}
 novaEng := nova.NewEngine(novaParams)
```

## 🔧 Configuration Presets

```go
// Mainnet (21 validators)
Mainnet = Parameters{K:21, AlphaPreference:13, AlphaConfidence:18, Beta:8}
// Testnet (11 validators)
Testnet = Parameters{K:11, AlphaPreference:7, AlphaConfidence:9, Beta:6}
// Local (5 validators)
Local   = Parameters{K:5,  AlphaPreference:4, AlphaConfidence:4, Beta:6}
```

## 📖 Citing

```bibtex
@software{lux_consensus_2025,
  author    = {Lux Industries Inc},
  title     = {Lux Consensus v1.0},
  year      = {2025},
  publisher = {},
  doi       = {},
}
```

## 📝 License

BSD 3‑Clause — free for academic & commercial use.
