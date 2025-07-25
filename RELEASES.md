# Release Notes

[v1.13.2] (July 2025) - Mainnet Stability Enhancements

This optional update is backwards compatible with v1.13.0. Plugin version updated to 41—update all plugins.

Key Changes:

    APIs: Added HTTP2 support for VMs; removed gzip for HTTP requests; enhanced Teleport protocol for faster cross-chain queries.

    Fixes: Resolved message timeouts on L1s; fixed ARM64 profiling issues; optimized historical data ranges with Teleport integration.

    Lux-Specific: Aligned with Avalanche's updates while adding Teleport-based load testing for mainnet robustness. New contributors: @mpignatelli12 and @alarso16.

    Full Changelog: Compare v1.13.1...v1.13.2 on Lux GitHub.

[v1.13.1] (June 2025) - Mainnet Prep and Teleport Refinements

Backwards compatible with v1.13.0. Plugin version to 40—update plugins.

Key Changes:

    APIs: Removed deprecated XVM APIs; added L1 validators to GetCurrentValidators.

    Configs: Removed tracing-enabled flag; deprecated XVM indexer configs.

    Lux-Specific: Incorporated Teleport for subnet configs; updated to match Avalanche's Etna while adding Lux's kube support for mainnet deployment. Many new contributors joined.

    Full Changelog: Compare v1.13.0...v1.13.1.

[v1.13.0] (May 2025) - Mainnet Launch with Teleport Dynamic Fees

Activates Lux Community Proposal (LCP) equivalent to LP-176: Dynamic EVM Gas Limits via Teleport.

Activation: Effective May 2025 on Lux Mainnet—upgrade before then.
Key Changes:

    APIs: Added ProposerVM timestamp metrics; network health checks for validators.

    Configs: Added min-block-delay and ingress connection grace period.

    Lux-Specific: Integrated Teleport for flexible gas mechanisms; aligned with Avalanche while adding custom kube deployment for mainnet scalability.

    Full Changelog: Compare v1.12.2...v1.13.0.

[v1.12.2] (April 2025) - Post-Mainnet Tweaks

Backwards compatible with v1.12.0. Plugin version to 39—update plugins. Removes deprecated Keystore API.

Key Changes:

    APIs: Added L1 validator fees; removed keystore methods.

    Configs: Removed static fee flags and keystore-enabled.

    Lux-Specific: Enhanced Teleport for ICM (Inter-Chain Messaging); focused on mainnet stability post-launch.

    Full Changelog: Compare v1.12.1...v1.12.2.

[v1.12.1] (March 2025) - Mainnet Beta Stabilization

Backwards compatible with v1.12.0.

Key Changes:

    Configs: Added pebble DB sync option.

    Fixes: Limited tx gas in P-Chain mempool.

    Lux-Specific: Teleport protocol tweaks for mainnet readiness.

    Full Changelog: Compare v1.12.0...v1.12.1.

[v1.12.0] (February 2025) - Mainnet Rollout

Activates multiple LCPs (Lux equivalents of LPs like 77, 103, etc.) effective February 2025.

Key Changes:

    APIs: Allowed issuing txs during partial sync.

    Lux-Specific: Full Teleport integration for warp messaging and dynamic fees in mainnet.

    Full Changelog: Compare v1.11.13...v1.12.0.

[v1.11.13] (January 2025) - Pre-Mainnet Optimizations

Backwards compatible with v1.11.0. Plugin version to 38.

Key Changes:

    APIs: Added getL1Validator and getProposedHeight.

    Configs: Added L1 cache sizes.

    Fixes: Metrics init in RPCChainVM; BLS components for Teleport.

    Lux-Specific: Prepared for mainnet with Teleport BLS support.

    Full Changelog: Compare v1.11.12...v1.11.13.

[v1.11.11] (December 2025) - Beta Enhancements

Backwards compatible with v1.11.0. Plugin version to 37.

Key Changes:

    APIs: Updated JSON marshalling; added info.upgrades.

    Configs: Added dynamic fees params.

    Fixes: Tracing panic; duplicate sig verifications.

    Lux-Specific: Teleport-aligned dynamic fees for beta testing.

    Full Changelog: Compare v1.11.10...v1.11.11.

[v1.11.10] (November 2025) - Beta Networking Fixes

Backwards compatible with v1.11.0. Plugin version to 36.

Key Changes:

    APIs: Renamed metrics for consistency.

    Fixes: Local validator start times; block building timers.

    Lux-Specific: Teleport protocol for kube deployments in beta.

    Full Changelog: Compare v1.11.9...v1.11.10.

[v1.11.9] (October 2025) - Beta Stability

Backwards compatible with v1.11.0. Plugin version to 35.

Key Changes:

    APIs: Updated health metrics to labels.

    Fixes: Tracing panic; ledger dependency.

    Lux-Specific: Teleport for beta interchain ops.

    Full Changelog: Compare v1.11.8...v1.11.9.

[v1.11.8] (September 2025) - Beta Metrics Overhaul

Backwards compatible with v1.11.0.

Key Changes:

    APIs: Switched metrics to labels.

    Lux-Specific: Teleport metrics for beta monitoring.

    Full Changelog: Compare v1.11.7...v1.11.8.

[v1.11.7] (August 2025) - Beta Pruning and Metrics

Backwards compatible with v1.11.0. Plugin version to 35.

Key Changes:

    APIs: Updated cache and DB metrics.

    Fixes: Bootstrapping performance; peer tracking.

    Lux-Specific: Teleport for beta subnet pruning.

    Full Changelog: Compare v1.11.6...v1.11.7.

[v1.11.6] (July 2025) - Beta Consensus Tweaks

Backwards compatible with v1.11.0.

Key Changes:

    APIs: Vectorized metrics for better tracking.

    Fixes: Mempool gossip; ETA calculations.

    Lux-Specific: Teleport in beta for consensus.

    Full Changelog: Compare v1.11.5...v1.11.6.

[v1.11.5] (June 2025) - Beta Gossip Improvements

Backwards compatible with v1.11.0.

Key Changes:

    APIs: Added BLS health checks.

    Fixes: Bootstrapping peer selection; topological sort.

    Lux-Specific: Teleport gossip in beta.

    Full Changelog: Compare v1.11.4...v1.11.5.

[v1.11.4] (May 2025) - Beta Sync Optimizations

Backwards compatible with v1.11.0.

Key Changes:

    APIs: Added finer-grained tracing.

    Fixes: P2P SDK cancellations; crash recovery.

    Lux-Specific: Teleport sync in beta.

    Full Changelog: Compare v1.11.3...v1.11.4.

[v1.11.3] (April 2025) - Beta API Removals

Backwards compatible with v1.11.0. Plugin version to 35.

Key Changes:

    APIs: Removed GetPendingValidators.

    Configs: Added networking params.

    Fixes: P2P validator sampling.

    Lux-Specific: Teleport for beta networking.

    Full Changelog: Compare v1.11.2...v1.11.3.

[v1.11.2] (March 2025) - Beta Gossip Redesign

Backwards compatible with v1.11.0. Plugin version to 34.

Key Changes:

    APIs: Removed IPC and auth; added gossip metrics.

    Configs: Added push-gossip params.

    Fixes: Gossip amplification.

    Lux-Specific: Teleport gossip in beta.

    Full Changelog: Compare v1.11.0...v1.11.2.

[v1.11.0] (February 2025) - Beta LCP Activations

Activates Lux equivalents of LPs (e.g., Warp Messaging).

Key Changes:

    APIs: Added getSubnet.

    Configs: Deprecated IPCs.

    Fixes: P-Chain shutdown deadlock.

    Lux-Specific: Teleport for beta cross-chain.

    Full Changelog: Compare v1.10.19...v1.11.0.

[v1.10.19] (January 2025) - Beta Prep

Backwards compatible with v1.10.0.

Key Changes:

    APIs: Added admin.dbGet; bloom metrics.

    Fixes: Validator set race; C-Chain metrics.

    Lux-Specific: Teleport prep for beta.

    Full Changelog: Compare v1.10.18...v1.10.19.

[v1.10.18] (December 2023) - Alpha to Beta Transition

Backwards compatible with v1.10.0. Plugin version to 31.

Key Changes:

    APIs: Added info.lps; metrics updates.

    Configs: Added peer-list frequencies.

    Fixes: Golang updates for CVEs.

    Lux-Specific: Teleport in alpha-beta shift.

    Full Changelog: Compare v1.10.17...v1.10.18.

For older releases (pre-2023 alpha), refer to Lux GitHub archives. If this is related to your previous query on LP-176, note that Lux's v1.13.0 incorporates a similar dynamic gas mechanism via Teleport, adapted for mainnet. For more details or comparisons, provide additional context!
