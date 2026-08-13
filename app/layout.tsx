import type { Metadata } from "next";
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

export const metadata: Metadata = {
  metadataBase: new URL("https://permit.example"),
  title: "Permit — Authorized access, open to everyone",
  description:
    "Open websites and services you own or are authorized to use through a simple, secure access gateway.",
  icons: {
    icon: "/favicon.svg",
    shortcut: "/favicon.svg",
  },
  openGraph: {
    title: "Permit — One address. Your authorized network.",
    description: "Free, secure access to resources you own or are allowed to use.",
    images: ["/og.png"],
  },
  twitter: {
    card: "summary_large_image",
    title: "Permit — One address. Your authorized network.",
    description: "Free, secure access to resources you own or are allowed to use.",
    images: ["/og.png"],
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body
        className={`${geistSans.variable} ${geistMono.variable} antialiased`}
      >
        {children}
      </body>
    </html>
  );
}
