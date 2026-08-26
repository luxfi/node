import "./global.css"
import { RootProvider } from "fumadocs-ui/provider/next"
import { ZenSans } from '@hanzo/font'
import type { ReactNode } from "react"

const inter = ZenSans

const interMono = ZenSans

export const metadata = {
  title: {
    default: "Lux Node Documentation",
    template: "%s | Lux Node",
  },
  description: "Core blockchain node software with multi-consensus support",
}

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <html
      lang="en"
      className={`${inter.variable} ${interMono.variable}`}
      suppressHydrationWarning
    >
      <body className="min-h-svh bg-background font-sans antialiased">
        <RootProvider
          search={{
            enabled: true,
          }}
          theme={{
            enabled: true,
            defaultTheme: "dark",
          }}
        >
          <div className="relative flex min-h-svh flex-col bg-background">
            {children}
          </div>
        </RootProvider>
      </body>
    </html>
  )
}
