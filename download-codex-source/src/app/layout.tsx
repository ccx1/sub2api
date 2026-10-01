import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Codex下载官网 - OpenAI Codex 一键安装、客户端、CLI、Windows/Mac/Linux 安装指南",
  description:
    "整理 OpenAI Codex 官网入口、Codex 一键安装命令、客户端下载资源、Windows / macOS / Linux 安装说明、CLI、VS Code、Cursor 与 Windsurf 接入教程。",
  metadataBase: new URL("https://codex.download.icodett.xyz"),
  alternates: {
    canonical: "https://codex.download.icodett.xyz/",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
