import { mkdir, writeFile, readFile, access } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import path from 'node:path';

const ORIGIN = 'https://codexdown.cn';
const PUBLIC_HOST = 'https://codex.download.icodett.xyz';
const ROOT = process.cwd();
const OUT = path.join(ROOT, 'public', 'mirror');
const PAGES = path.join(OUT, 'pages');
const ASSETS = path.join(OUT, 'assets');
const META = path.join(ROOT, 'docs', 'research', 'codexdown');
const ua = `Mozilla/5.0 (compatible; HermesClone/1.0; +${PUBLIC_HOST})`;

async function exists(p){ try { await access(p); return true; } catch { return false; } }
async function ensureDir(p){ await mkdir(p,{recursive:true}); }
function sleep(ms){ return new Promise(r=>setTimeout(r,ms)); }
function pageKey(url){
  const u = new URL(url);
  let pathname = u.pathname;
  if (!pathname.endsWith('/')) pathname += '/';
  const q = u.search ? '__q_' + createHash('sha1').update(u.search).digest('hex').slice(0,12) : '';
  return path.join(PAGES, pathname, q + 'index.html');
}
function assetName(url){
  const u = new URL(url, ORIGIN);
  const ext = path.extname(u.pathname).slice(0,12) || '.bin';
  const base = path.basename(u.pathname, ext).replace(/[^a-zA-Z0-9._-]/g,'-').slice(0,80) || 'asset';
  const hash = createHash('sha1').update(u.href).digest('hex').slice(0,12);
  return `/mirror/assets/${base}-${hash}${ext}`;
}
async function fetchBuffer(url, attempt=1){
  const ctrl = new AbortController();
  const t = setTimeout(()=>ctrl.abort(), 25000);
  try {
    const res = await fetch(url,{headers:{'user-agent':ua}, signal: ctrl.signal, redirect:'follow'});
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const arr = await res.arrayBuffer();
    return {buf:Buffer.from(arr), type:res.headers.get('content-type')||''};
  } catch(e) {
    if (attempt < 3) { await sleep(800*attempt); return fetchBuffer(url, attempt+1); }
    throw e;
  } finally { clearTimeout(t); }
}
function rewriteHtml(html, sourceUrl, assetsMap){
  let out = html;
  out = out.replaceAll('https://codexdown.cn', PUBLIC_HOST);
  out = out.replaceAll('http://codexdown.cn', PUBLIC_HOST);
  out = out.replace(/(href|src)=(['"])(\.\.?\/[^'"#?]+)([^'"]*)\2/g, (m, attr, q, rel, suffix) => {
    try {
      const abs = new URL(rel + suffix, sourceUrl).href;
      if (assetsMap.has(abs.split('#')[0])) return `${attr}=${q}${assetsMap.get(abs.split('#')[0])}${q}`;
      const u = new URL(abs);
      if (u.origin === ORIGIN) return `${attr}=${q}${u.pathname}${u.search}${u.hash}${q}`;
    } catch {}
    return m;
  });
  out = out.replace(/(href|src)=(['"])(\/[^'"#?]+)([^'"]*)\2/g, (m, attr, q, p, suffix) => {
    const abs = ORIGIN + p + suffix;
    if (assetsMap.has(abs.split('#')[0])) return `${attr}=${q}${assetsMap.get(abs.split('#')[0])}${q}`;
    return m;
  });
  out = out.replace(/url\((['"]?)(\.\.?\/[^)'"?]+)([^)'"]*)\1\)/g, (m, q, rel, suffix) => {
    try { const abs = new URL(rel + suffix, sourceUrl).href; if (assetsMap.has(abs.split('#')[0])) return `url(${assetsMap.get(abs.split('#')[0])})`; } catch {}
    return m;
  });
  out = out.replace(/url\((['"]?)(\/[^)'"?]+)([^)'"]*)\1\)/g, (m, q, p, suffix) => {
    const abs = ORIGIN + p + suffix; if (assetsMap.has(abs.split('#')[0])) return `url(${assetsMap.get(abs.split('#')[0])})`; return m;
  });
  out = out.replace(/<\/head>/i, `\n<script>window.ORIGINAL_SITE='${ORIGIN}';window.CLONE_SITE='${PUBLIC_HOST}';</script>\n</head>`);
  return out;
}
function extractAssetUrls(html, sourceUrl){
  const urls = new Set();
  const attrRe = /(?:src|href)=(['"])([^'"#]+)\1/gi;
  let m;
  while ((m=attrRe.exec(html))) {
    const raw=m[2];
    if (/^(mailto:|tel:|javascript:|#)/i.test(raw)) continue;
    try {
      const u = new URL(raw, sourceUrl);
      if (u.origin !== ORIGIN) continue;
      const ext = path.extname(u.pathname).toLowerCase();
      if (['.png','.jpg','.jpeg','.webp','.gif','.svg','.ico','.css','.js','.mjs','.mp4','.woff','.woff2','.ttf','.json','.txt','.xml','.webmanifest'].includes(ext) || u.pathname.includes('/_astro/') || u.pathname.includes('/assets/')) urls.add(u.href);
    } catch {}
  }
  const urlRe = /url\((['"]?)([^)'"]+)\1\)/gi;
  while ((m=urlRe.exec(html))) {
    try { const u = new URL(m[2], sourceUrl); if (u.origin===ORIGIN) urls.add(u.href); } catch {}
  }
  return [...urls];
}
async function main(){
  await ensureDir(PAGES); await ensureDir(ASSETS); await ensureDir(META);
  const sitemapMain = (await fetchBuffer(`${ORIGIN}/sitemap.xml`)).buf.toString('utf8');
  let sitemapDocs = '';
  try { sitemapDocs = (await fetchBuffer(`${ORIGIN}/docs/sitemap.xml`)).buf.toString('utf8'); } catch {}
  const urls = [...(sitemapMain + '\n' + sitemapDocs).matchAll(/<loc>(.*?)<\/loc>/g)].map(m=>m[1].trim()).filter(u=>u.startsWith(ORIGIN));
  // Include manual top-level files.
  const extra = [`${ORIGIN}/`,`${ORIGIN}/docs/`,`${ORIGIN}/articles/`,`${ORIGIN}/updates/`,`${ORIGIN}/pets/`,`${ORIGIN}/robots.txt`,`${ORIGIN}/llms.txt`,`${ORIGIN}/docs/llms.txt`,`${ORIGIN}/docs/sitemap.xml`];
  const pageUrls = [...new Set([...urls, ...extra])];
  const assetSet = new Set();
  const pageResults = [];
  let idx=0;
  async function worker(){
    while(idx<pageUrls.length){
      const i=idx++; const url=pageUrls[i];
      const dest=pageKey(url);
      try{
        const {buf,type}=await fetchBuffer(url);
        let text=buf.toString('utf8');
        for (const a of extractAssetUrls(text,url)) assetSet.add(a.split('#')[0]);
        await ensureDir(path.dirname(dest));
        await writeFile(dest, text);
        pageResults.push({url,status:'ok',bytes:buf.length,type,dest:path.relative(ROOT,dest)});
        if ((i+1)%100===0) console.log('pages',i+1,'/',pageUrls.length,'assets',assetSet.size);
      }catch(e){ pageResults.push({url,status:'error',error:String(e)}); console.warn('page failed',url,String(e)); }
    }
  }
  await Promise.all(Array.from({length:10},worker));
  const assets = [...assetSet];
  const assetsMap = new Map();
  let aidx=0, okAssets=0, failAssets=0;
  async function assetWorker(){
    while(aidx<assets.length){
      const i=aidx++; const url=assets[i];
      const local=assetName(url); assetsMap.set(url,local);
      const dest=path.join(ROOT,'public',local);
      if (await exists(dest)) { okAssets++; continue; }
      try { const {buf}=await fetchBuffer(url); await ensureDir(path.dirname(dest)); await writeFile(dest,buf); okAssets++; }
      catch(e){ failAssets++; console.warn('asset failed',url,String(e)); }
      if ((i+1)%50===0) console.log('assets',i+1,'/',assets.length);
    }
  }
  await Promise.all(Array.from({length:8},assetWorker));
  // Rewrite pages after asset map is known.
  for (const r of pageResults) if (r.status==='ok') {
    const file=path.join(ROOT,r.dest); const text=await readFile(file,'utf8');
    await writeFile(file, rewriteHtml(text,r.url,assetsMap));
  }
  await writeFile(path.join(META,'mirror-manifest.json'), JSON.stringify({origin:ORIGIN, publicHost:PUBLIC_HOST, pages:pageResults, assets:assets.map(u=>({url:u,local:assetsMap.get(u)})), okAssets, failAssets},null,2));
  await writeFile(path.join(META,'PAGE_TOPOLOGY.md'), `# codexdown.cn topology\n\nMirrored ${pageResults.filter(r=>r.status==='ok').length}/${pageUrls.length} public sitemap/text pages plus ${okAssets}/${assets.length} discovered assets. Top-level sections include home, updates, pets, docs, articles, and article/doc detail pages. Route handling is file-backed and rewrites internal links to ${new URL(PUBLIC_HOST).host} while preserving external download links.\n`);
  await writeFile(path.join(META,'BEHAVIORS.md'), `# codexdown.cn behaviors\n\nThe mirrored site preserves the original public HTML/CSS/JS, including Tailwind-generated classes, Astro docs search bundle, hover transitions, gradient backgrounds, sticky/navigation behavior, and client scripts from public assets.\n`);
  console.log('DONE pages',pageResults.filter(r=>r.status==='ok').length,'/',pageUrls.length,'assets',okAssets,'/',assets.length,'failedAssets',failAssets);
}
main().catch(e=>{console.error(e); process.exit(1)});
