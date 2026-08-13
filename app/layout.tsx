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
  title: "Permit — Open a registered public resource",
  description:
    "Open public web resources that have been pre-registered with Permit.",
  icons: {
    icon: "/favicon.svg",
    shortcut: "/favicon.svg",
  },
  openGraph: {
    title: "Permit — Open a registered public resource",
    description: "Open public web resources that have been pre-registered with Permit.",
    images: ["/og.png"],
  },
  twitter: {
    card: "summary_large_image",
    title: "Permit — Open a registered public resource",
    description: "Open public web resources that have been pre-registered with Permit.",
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
