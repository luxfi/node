import Link from "next/link";

export default function HomePage() {
  return (
    <main className="min-h-screen">
      {/* Hero Section */}
      <section className="relative overflow-hidden border-b fd-border">
        <div className="absolute inset-0 fd-background opacity-50" />
        <div className="relative mx-auto max-w-6xl px-6 py-24 text-center">
          <h1 className="text-5xl font-bold tracking-tight fd-foreground sm:text-6xl">
            Lux Node
          </h1>
          <p className="mx-auto mt-6 max-w-2xl text-lg fd-muted-foreground">
            High-performance blockchain node software powering the Lux Network.
            Multi-chain architecture with P-Chain validators, C-Chain EVM smart contracts,
            and X-Chain asset exchange built on novel consensus protocols.
          </p>
          <div className="mt-10 flex items-center justify-center gap-4">
            <Link
              href="/docs"
              className="rounded-lg px-6 py-3 font-medium fd-primary text-white transition hover:opacity-90"
              style={{ backgroundColor: "hsl(var(--primary))" }}
            >
              Read Documentation
            </Link>
            <Link
              href="https://github.com/luxfi/node"
              className="rounded-lg border px-6 py-3 font-medium fd-border fd-foreground transition hover:fd-muted"
            >
              View on GitHub
            </Link>
          </div>
        </div>
      </section>

      {/* Quick Install Section */}
      <section className="border-b fd-border py-16">
        <div className="mx-auto max-w-6xl px-6">
          <h2 className="text-center text-2xl font-semibold fd-foreground">
            Quick Install
          </h2>
          <p className="mt-2 text-center fd-muted-foreground">
            Install the Lux node with a single command
          </p>
          <div className="mt-8 flex justify-center">
            <div className="relative w-full max-w-2xl">
              <pre className="overflow-x-auto rounded-lg border fd-border fd-card p-4">
                <code className="text-sm fd-foreground">
                  go install github.com/luxfi/node@latest
                </code>
              </pre>
              <div className="absolute right-3 top-3">
                <button
                  className="rounded p-2 fd-muted-foreground transition hover:fd-foreground"
                  title="Copy to clipboard"
                >
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    width="16"
                    height="16"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  >
                    <rect width="14" height="14" x="8" y="8" rx="2" ry="2" />
                    <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
                  </svg>
                </button>
              </div>
            </div>
          </div>
          <div className="mt-6 flex justify-center gap-4 text-sm fd-muted-foreground">
            <span>Requires Go 1.21+</span>
            <span>|</span>
            <Link href="/docs/installation" className="underline hover:fd-foreground">
              Installation Guide
            </Link>
          </div>
        </div>
      </section>

      {/* Features Grid */}
      <section className="py-20">
        <div className="mx-auto max-w-6xl px-6">
          <h2 className="text-center text-3xl font-bold fd-foreground">
            Core Components
          </h2>
          <p className="mx-auto mt-4 max-w-2xl text-center fd-muted-foreground">
            A multi-chain architecture designed for scalability, security, and interoperability
          </p>
          <div className="mt-12 grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
            {/* P-Chain */}
            <Link
              href="/docs/pchain"
              className="group rounded-xl border fd-border fd-card p-6 transition hover:border-blue-500/50 hover:shadow-lg"
            >
              <div className="flex h-12 w-12 items-center justify-center rounded-lg bg-blue-500/10">
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  className="text-blue-500"
                >
                  <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10" />
                </svg>
              </div>
              <h3 className="mt-4 text-xl font-semibold fd-foreground group-hover:text-blue-500">
                P-Chain
              </h3>
              <p className="mt-2 fd-muted-foreground">
                Platform chain for validator coordination, staking, and chain management.
                Linear consensus with 100-year vesting schedules.
              </p>
            </Link>

            {/* C-Chain */}
            <Link
              href="/docs/cchain"
              className="group rounded-xl border fd-border fd-card p-6 transition hover:border-green-500/50 hover:shadow-lg"
            >
              <div className="flex h-12 w-12 items-center justify-center rounded-lg bg-green-500/10">
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  className="text-green-500"
                >
                  <rect width="18" height="18" x="3" y="3" rx="2" />
                  <path d="M7 7h.01" />
                  <path d="M17 7h.01" />
                  <path d="M7 17h.01" />
                  <path d="M17 17h.01" />
                </svg>
              </div>
              <h3 className="mt-4 text-xl font-semibold fd-foreground group-hover:text-green-500">
                C-Chain
              </h3>
              <p className="mt-2 fd-muted-foreground">
                EVM-compatible smart contract chain. Full Ethereum tooling support with
                Chain ID 96369 and post-quantum precompiles.
              </p>
            </Link>

            {/* X-Chain */}
            <Link
              href="/docs/xchain"
              className="group rounded-xl border fd-border fd-card p-6 transition hover:border-purple-500/50 hover:shadow-lg"
            >
              <div className="flex h-12 w-12 items-center justify-center rounded-lg bg-purple-500/10">
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  className="text-purple-500"
                >
                  <circle cx="12" cy="12" r="10" />
                  <path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20" />
                  <path d="M2 12h20" />
                </svg>
              </div>
              <h3 className="mt-4 text-xl font-semibold fd-foreground group-hover:text-purple-500">
                X-Chain
              </h3>
              <p className="mt-2 fd-muted-foreground">
                Asset exchange chain with DAG consensus. Fast UTXO-based transactions
                for token creation and cross-chain transfers.
              </p>
            </Link>

            {/* Consensus */}
            <Link
              href="/docs/consensus"
              className="group rounded-xl border fd-border fd-card p-6 transition hover:border-orange-500/50 hover:shadow-lg"
            >
              <div className="flex h-12 w-12 items-center justify-center rounded-lg bg-orange-500/10">
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  className="text-orange-500"
                >
                  <path d="M12 3v18" />
                  <path d="M18.5 8.5 12 3 5.5 8.5" />
                  <path d="m5.5 15.5 6.5 5.5 6.5-5.5" />
                </svg>
              </div>
              <h3 className="mt-4 text-xl font-semibold fd-foreground group-hover:text-orange-500">
                Consensus
              </h3>
              <p className="mt-2 fd-muted-foreground">
                Novel consensus protocols including Quasar quantum-safe finality,
                BFT for C-Chain, and DAG for X-Chain operations.
              </p>
            </Link>

            {/* Networking */}
            <Link
              href="/docs/networking"
              className="group rounded-xl border fd-border fd-card p-6 transition hover:border-cyan-500/50 hover:shadow-lg"
            >
              <div className="flex h-12 w-12 items-center justify-center rounded-lg bg-cyan-500/10">
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  className="text-cyan-500"
                >
                  <rect x="16" y="16" width="6" height="6" rx="1" />
                  <rect x="2" y="16" width="6" height="6" rx="1" />
                  <rect x="9" y="2" width="6" height="6" rx="1" />
                  <path d="M5 16v-3a1 1 0 0 1 1-1h12a1 1 0 0 1 1 1v3" />
                  <path d="M12 12V8" />
                </svg>
              </div>
              <h3 className="mt-4 text-xl font-semibold fd-foreground group-hover:text-cyan-500">
                Networking
              </h3>
              <p className="mt-2 fd-muted-foreground">
                P2P networking layer with peer discovery, message routing,
                and cross-chain Warp messaging for chain communication.
              </p>
            </Link>

            {/* APIs */}
            <Link
              href="/docs/api"
              className="group rounded-xl border fd-border fd-card p-6 transition hover:border-pink-500/50 hover:shadow-lg"
            >
              <div className="flex h-12 w-12 items-center justify-center rounded-lg bg-pink-500/10">
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  className="text-pink-500"
                >
                  <path d="m18 16 4-4-4-4" />
                  <path d="m6 8-4 4 4 4" />
                  <path d="m14.5 4-5 16" />
                </svg>
              </div>
              <h3 className="mt-4 text-xl font-semibold fd-foreground group-hover:text-pink-500">
                APIs
              </h3>
              <p className="mt-2 fd-muted-foreground">
                JSON-RPC and REST APIs for all chains. EVM-compatible endpoints,
                admin APIs, and health monitoring interfaces.
              </p>
            </Link>
          </div>
        </div>
      </section>

      {/* Common Commands Section */}
      <section className="border-t fd-border py-20">
        <div className="mx-auto max-w-6xl px-6">
          <h2 className="text-center text-3xl font-bold fd-foreground">
            Common Commands
          </h2>
          <p className="mx-auto mt-4 max-w-2xl text-center fd-muted-foreground">
            Get started quickly with these essential node operations
          </p>
          <div className="mt-12 grid gap-6 lg:grid-cols-2">
            {/* Start Node */}
            <div className="rounded-xl border fd-border fd-card p-6">
              <h3 className="font-semibold fd-foreground">Start a Local Node</h3>
              <p className="mt-2 text-sm fd-muted-foreground">
                Launch a node on the local network for development
              </p>
              <pre className="mt-4 overflow-x-auto rounded-lg bg-black/50 p-4">
                <code className="text-sm text-green-400">
                  luxd --network-id=local
                </code>
              </pre>
            </div>

            {/* Join Testnet */}
            <div className="rounded-xl border fd-border fd-card p-6">
              <h3 className="font-semibold fd-foreground">Join Testnet</h3>
              <p className="mt-2 text-sm fd-muted-foreground">
                Connect to the Lux testnet for testing
              </p>
              <pre className="mt-4 overflow-x-auto rounded-lg bg-black/50 p-4">
                <code className="text-sm text-green-400">
                  luxd --network-id=testnet
                </code>
              </pre>
            </div>

            {/* Join Mainnet */}
            <div className="rounded-xl border fd-border fd-card p-6">
              <h3 className="font-semibold fd-foreground">Join Mainnet</h3>
              <p className="mt-2 text-sm fd-muted-foreground">
                Connect to the production Lux mainnet
              </p>
              <pre className="mt-4 overflow-x-auto rounded-lg bg-black/50 p-4">
                <code className="text-sm text-green-400">
                  luxd --network-id=mainnet
                </code>
              </pre>
            </div>

            {/* Check Health */}
            <div className="rounded-xl border fd-border fd-card p-6">
              <h3 className="font-semibold fd-foreground">Check Node Health</h3>
              <p className="mt-2 text-sm fd-muted-foreground">
                Verify your node is running and healthy
              </p>
              <pre className="mt-4 overflow-x-auto rounded-lg bg-black/50 p-4">
                <code className="text-sm text-green-400">
                  curl -X POST --data &#39;&#123;&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;health.health&quot;,&quot;id&quot;:1&#125;&#39; \{"\n"}  -H &#39;content-type:application/json&#39; \{"\n"}  127.0.0.1:9650/v1/health
                </code>
              </pre>
            </div>
          </div>
        </div>
      </section>

      {/* Footer */}
      <footer className="border-t fd-border py-12">
        <div className="mx-auto max-w-6xl px-6">
          <div className="grid gap-8 sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <h4 className="font-semibold fd-foreground">Documentation</h4>
              <ul className="mt-4 space-y-2 text-sm fd-muted-foreground">
                <li>
                  <Link href="/docs" className="hover:fd-foreground">
                    Getting Started
                  </Link>
                </li>
                <li>
                  <Link href="/docs/installation" className="hover:fd-foreground">
                    Installation
                  </Link>
                </li>
                <li>
                  <Link href="/docs/configuration" className="hover:fd-foreground">
                    Configuration
                  </Link>
                </li>
                <li>
                  <Link href="/docs/api" className="hover:fd-foreground">
                    API Reference
                  </Link>
                </li>
              </ul>
            </div>
            <div>
              <h4 className="font-semibold fd-foreground">Chains</h4>
              <ul className="mt-4 space-y-2 text-sm fd-muted-foreground">
                <li>
                  <Link href="/docs/pchain" className="hover:fd-foreground">
                    P-Chain (Platform)
                  </Link>
                </li>
                <li>
                  <Link href="/docs/cchain" className="hover:fd-foreground">
                    C-Chain (Contracts)
                  </Link>
                </li>
                <li>
                  <Link href="/docs/xchain" className="hover:fd-foreground">
                    X-Chain (Exchange)
                  </Link>
                </li>
                <li>
                  <Link href="/docs/chains" className="hover:fd-foreground">
                    Chains
                  </Link>
                </li>
              </ul>
            </div>
            <div>
              <h4 className="font-semibold fd-foreground">Resources</h4>
              <ul className="mt-4 space-y-2 text-sm fd-muted-foreground">
                <li>
                  <Link href="https://github.com/luxfi/node" className="hover:fd-foreground">
                    GitHub
                  </Link>
                </li>
                <li>
                  <Link href="https://lux.network" className="hover:fd-foreground">
                    Lux Network
                  </Link>
                </li>
                <li>
                  <Link href="https://explorer.lux.network" className="hover:fd-foreground">
                    Block Explorer
                  </Link>
                </li>
                <li>
                  <Link href="/docs/faq" className="hover:fd-foreground">
                    FAQ
                  </Link>
                </li>
              </ul>
            </div>
            <div>
              <h4 className="font-semibold fd-foreground">Community</h4>
              <ul className="mt-4 space-y-2 text-sm fd-muted-foreground">
                <li>
                  <Link href="https://discord.gg/lux" className="hover:fd-foreground">
                    Discord
                  </Link>
                </li>
                <li>
                  <Link href="https://twitter.com/luxnetwork" className="hover:fd-foreground">
                    Twitter
                  </Link>
                </li>
                <li>
                  <Link href="https://t.me/luxnetwork" className="hover:fd-foreground">
                    Telegram
                  </Link>
                </li>
                <li>
                  <Link href="/docs/contributing" className="hover:fd-foreground">
                    Contributing
                  </Link>
                </li>
              </ul>
            </div>
          </div>
          <div className="mt-12 border-t fd-border pt-8 text-center text-sm fd-muted-foreground">
            <p>
              Built by{" "}
              <Link href="https://lux.network" className="underline hover:fd-foreground">
                Lux Industries
              </Link>
              . Licensed under BSD-3-Clause.
            </p>
          </div>
        </div>
      </footer>
    </main>
  );
}
