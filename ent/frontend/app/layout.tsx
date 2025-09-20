import type { Metadata } from "next";
import "./styles/globals.css";

export const metadata: Metadata = {
  title: "gai Chat",
  description: "Chat experience powered by the gai Go SDK"
};

export default function RootLayout({
  children
}: {
  children: React.ReactNode;
}): JSX.Element {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
