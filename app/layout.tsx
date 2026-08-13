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
  metadataBase: new URL("https://8.219.11.175"),
  title: "OWU — Open Website Unblocker",
  description:
    "A browser-password protected personal web proxy.",
  icons: {
    icon: "/favicon.svg",
    shortcut: "/favicon.svg",
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
