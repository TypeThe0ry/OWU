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

const siteUrl = process.env.NEXT_PUBLIC_OWU_BASE_URL ?? "https://owu.terracat.net";

export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  applicationName: "OWU",
  title: {
    default: "OWU — Open Website Unblocker",
    template: "%s · OWU",
  },
  description:
    "OWU is a self-hosted personal web proxy. Open any HTTP or HTTPS website through your own server while the address bar stays on OWU — built by Team TerraCat.",
  keywords: [
    "web proxy",
    "unblocker",
    "personal proxy",
    "open website",
    "OWU",
    "Open Website Unblocker",
    "Team TerraCat",
    "browser proxy",
  ],
  authors: [{ name: "Team TerraCat", url: "https://github.com/TypeThe0ry" }],
  creator: "Team TerraCat",
  publisher: "Team TerraCat",
  robots: { index: true, follow: true, maxImagePreview: "large" },
  alternates: { canonical: "/" },
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
    description:
      "Your private browser-based web proxy. Open any website through your server — built by Team TerraCat.",
    url: siteUrl,
    siteName: "OWU",
    locale: "en_US",
    type: "website",
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

const websiteLd = {
  "@context": "https://schema.org",
  "@type": "WebSite",
  "@id": siteUrl + "/#website",
  url: siteUrl + "/",
  name: "OWU — Open Website Unblocker",
  alternateName: "OWU",
  description:
    "OWU is a self-hosted personal web proxy that opens any HTTP or HTTPS website through your own server.",
  inLanguage: "en",
  applicationCategory: "WebApplication",
  operatingSystem: "Any",
  publisher: {
    "@type": "Organization",
    name: "Team TerraCat",
    url: "https://github.com/TypeThe0ry",
  },
};

const themeInitScript = "try{const saved=localStorage.getItem('owu-theme');const theme=saved==='light'||saved==='dark'?saved:(matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light');document.documentElement.dataset.theme=theme}catch{}";

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeInitScript }} />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: JSON.stringify(websiteLd) }}
        />
      </head>
      <body
        className={"".concat(geistSans.variable, " ", geistMono.variable, " antialiased")}
      >
        {children}
      </body>
    </html>
  );
}
