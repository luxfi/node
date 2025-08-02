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

[v1.13.0] (May 2025) - Etna Compatible, Lux Launch Ready

Major update aligning with Avalanche's Etna release. Plugin version 39—rebuild plugins.

Key Changes:

    Core: Integrated Etna network upgrade support with Lux-specific chain configs; adapted snow engine for Lux's 21-node consensus.

    VM Updates: Removed outdated XVM; added Teleport integration for mainnet communication across subnets.

    Lux-Specific: Replaced Avalanche branding; added genesis files for 21-node mainnet, 11-node testnet; enabled Teleport for cross-chain operations.

    Config Changes: New mainnet defaults optimized for 21 validators; adjusted consensus params for Lux's infrastructure.

    Deprecations: Removed Avalanche-specific features not needed for Lux.

    Full Changelog: Available on Lux GitHub release page.

Historical Notes: This is the first major release for Lux Network, forked from Avalanche v1.13.0 with substantial modifications for our unique architecture and consensus requirements.