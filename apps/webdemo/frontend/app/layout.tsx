import type { Metadata } from "next"
import "./globals.css"

export const metadata: Metadata = {
  title: "GAI Web Demo",
  description: "Explore OpenAI, Anthropic, and Gemini through the GAI SDK",
}

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  )
}
