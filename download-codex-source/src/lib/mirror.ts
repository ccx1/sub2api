import { readFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import path from "node:path";

const root = process.cwd();
const pagesRoot = path.join(root, "public", "mirror", "pages");

function queryKey(search: string): string {
  if (!search) return "index.html";
  const hash = createHash("sha1").update(search).digest("hex").slice(0, 12);
  return `__q_${hash}index.html`;
}

function contentTypeFor(pathname: string): string {
  if (pathname.endsWith(".xml")) return "application/xml; charset=utf-8";
  if (pathname.endsWith(".txt")) return "text/plain; charset=utf-8";
  if (pathname.endsWith(".json")) return "application/json; charset=utf-8";
  return "text/html; charset=utf-8";
}

function safePath(pathname: string, search: string): string {
  const cleanPath = pathname.replace(/\0/g, "");
  const normalized = path.posix.normalize(cleanPath.startsWith("/") ? cleanPath : `/${cleanPath}`);
  if (normalized.includes("..")) return path.join(pagesRoot, "__missing__");

  if (normalized === "/") return path.join(pagesRoot, "index.html");

  const looksLikeFile = /\.[a-zA-Z0-9]{1,8}$/.test(normalized);
  if (looksLikeFile) return path.join(pagesRoot, normalized);

  return path.join(pagesRoot, normalized, queryKey(search));
}

function baseIndexPath(pathname: string): string | null {
  const cleanPath = pathname.replace(/\0/g, "");
  const normalized = path.posix.normalize(cleanPath.startsWith("/") ? cleanPath : `/${cleanPath}`);
  if (normalized.includes("..")) return null;
  if (normalized === "/") return path.join(pagesRoot, "index.html");
  return path.join(pagesRoot, normalized, "index.html");
}

function mirroredHeaders(pathname: string, maxAge = 300): HeadersInit {
  return {
    "content-type": contentTypeFor(pathname),
    "cache-control": `public, max-age=${maxAge}, s-maxage=${maxAge}`,
    "x-cloned-from": "https://codexdown.cn",
  };
}

export async function mirroredResponse(request: Request): Promise<Response> {
  const url = new URL(request.url);
  const filePath = safePath(url.pathname, url.search);

  try {
    const body = await readFile(filePath);
    return new Response(body, { headers: mirroredHeaders(url.pathname) });
  } catch {
    // Client-rendered pages such as /pets/?page=2 only need the base /pets/ shell.
    // File-like mirrored pages such as /llms.txt are stored as /llms.txt/index.html.
    if (url.search || /\.[a-zA-Z0-9]{1,8}$/.test(url.pathname)) {
      try {
        const body = await readFile(baseIndexPath(url.pathname) ?? "");
        return new Response(body, { headers: mirroredHeaders(url.pathname) });
      } catch {
        // fall through to global 404 fallback
      }
    }

    const fallback = await readFile(path.join(pagesRoot, "index.html"));
    return new Response(fallback, {
      status: 404,
      headers: {
        "content-type": "text/html; charset=utf-8",
        "cache-control": "public, max-age=60",
        "x-cloned-from": "https://codexdown.cn",
      },
    });
  }
}
