import type { Metadata, Viewport } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  maximumScale: 5,
  userScalable: true,
};

export const metadata: Metadata = {
  title: "Loom - Manage Multiple AI Coding Agents",
  description: "A terminal app that manages multiple AI coding agents (Claude Code, Codex, Gemini, Aider) in separate workspaces, allowing you to work on multiple tasks simultaneously.",
  keywords: ["loom", "ai", "coding agent", "terminal", "tmux", "claude code", "codex", "gemini", "aider"],
  authors: [{ name: "aidan-bailey" }],
  openGraph: {
    title: "Loom",
    description: "A terminal app that manages multiple AI coding agents in separate workspaces",
    url: "https://github.com/aidan-bailey/loom",
    type: "website",
  },
  twitter: {
    card: "summary_large_image",
    title: "Loom",
    description: "A terminal app that manages multiple AI coding agents in separate workspaces",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body className={`${geistSans.variable} ${geistMono.variable}`}>
        {children}
      </body>
    </html>
  );
}