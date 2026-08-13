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

export const metadata: Metadata = {
  metadataBase: new URL(
    process.env.NEXT_PUBLIC_OWU_BASE_URL ?? "https://owu.example.com",
  ),
  applicationName: "OWU",
  title: "OWU — Open Website Unblocker",
  description:
    "A browser-password protected personal web proxy.",
  manifest: "/manifest.webmanifest",
  icons: {
    icon: [
      { url: "/favicon.ico", sizes: "any" },
      { url: "/favicon.svg", type: "image/svg+xml" },
      { url: "/favicon-32.png", sizes: "32x32", type: "image/png" },
    ],
    shortcut: "/favicon.ico",
    apple: [{ url: "/apple-touch-icon.png", sizes: "180x180", type: "image/png" }],
  },
  appleWebApp: {
    capable: true,
    title: "OWU",
    statusBarStyle: "black-translucent",
  },
  openGraph: {
    title: "OWU — Open Website Unblocker",
    description: "Your private browser-based web proxy.",
    images: ["/og.png"],
  },
  twitter: {
    card: "summary_large_image",
    title: "OWU — Open Website Unblocker",
    description: "Your private browser-based web proxy.",
    images: ["/og.png"],
  },
};

export const viewport: Viewport = {
  colorScheme: "light dark",
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#F7F8FF" },
    { media: "(prefers-color-scheme: dark)", color: "#070916" },
  ],
};

const themeInitScript = `try{const saved=localStorage.getItem("owu-theme");const theme=saved==="light"||saved==="dark"?saved:(matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light");document.documentElement.dataset.theme=theme}catch{}`;

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeInitScript }} />
      </head>
      <body
        className={`${geistSans.variable} ${geistMono.variable} antialiased`}
      >
        {children}
      </body>
    </html>
  );
}
