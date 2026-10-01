import fs from 'node:fs/promises';
import path from 'node:path';

const ROOT = process.cwd();
const pagesRoot = path.join(ROOT, 'public/mirror/pages');
const assetsRoot = path.join(ROOT, 'public/mirror/assets');
const PUBLIC_HOST = 'https://codex.download.icodett.xyz';
const PROXY_HOST = 'https://ai.icodett.xyz';
const PUBLIC_HOSTNAME = new URL(PUBLIC_HOST).host;
const PROXY_HOSTNAME = new URL(PROXY_HOST).host;
const OLD_HOSTNAME = ['download', 'yancc', 'cloud'].join('.');
const OLD_PUBLIC_HOST = `https://${OLD_HOSTNAME}`;
const OLD_PROXY_HOSTNAME = ['us', 'yancc', 'cloud'].join('.');
const OLD_PROXY_HOST = `https://${OLD_PROXY_HOSTNAME}`;
const QUARK_HOSTNAME = ['pan', 'quark', 'cn'].join('.');
const QUARK_SHARE_ID = '1ad15b67bc05';
const QUARK_SHARE_URL = `https://${QUARK_HOSTNAME}/s/${QUARK_SHARE_ID}`;
const QUARK_VISIBLE_SHARE_ID = 'c2fa1822a1ba';
const QUARK_VISIBLE_SHARE_PATH = `${QUARK_HOSTNAME}/s/${QUARK_VISIBLE_SHARE_ID}`;
const QUARK_VISIBLE_SHARE_URL = `https://${QUARK_VISIBLE_SHARE_PATH}`;
const LEGACY_QUARK_SHARE_IDS = [['c93d8', 'ea834ca'].join('')];
const DOWNLOAD_RESOURCE_TEXT = '\u4e0b\u8f7d\u8d44\u6e90\u7edf\u4e00\u6307\u5411';
const QUARK_DISK_TEXT = '\u5938\u514b\u7f51\u76d8';
const THIRD_PARTY_TEXT = '\u7b2c\u4e09\u65b9\u4e2d\u8f6c\u7ad9';
const API_TEXT = 'API';

const homepagePath = path.join(pagesRoot, 'index.html');
const petsDataPath = path.join(assetsRoot, 'pets-data-b9f312d4f0da.js');
const petsScriptPath = path.join(assetsRoot, 'pets-94e72ab5f8c9.js');
const petsCssPath = path.join(assetsRoot, 'pets-b8db8841e3ea.css');

async function* walk(dir) {
  for (const entry of await fs.readdir(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) yield* walk(full);
    else yield full;
  }
}

function preserveVisibleQuarkShare(input) {
  return input.replace(/<li>[\s\S]*?<\/li>/g, (item) => {
    if (!item.includes(DOWNLOAD_RESOURCE_TEXT)) return item;
    return item
      .replaceAll(THIRD_PARTY_TEXT, QUARK_DISK_TEXT)
      .replace(/href="https:\/\/ai\.icodett\.xyz\/?"/g, `href="${QUARK_VISIBLE_SHARE_URL}"`)
      .replace(/>ai\.icodett\.xyz<\/a>/g, `>${QUARK_VISIBLE_SHARE_PATH}</a>`);
  });
}

function anchorText(html) {
  return html
    .replace(/<[^>]*>/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

function isCodexDownloadAnchor(text) {
  if (!text) return false;
  if (text.includes(THIRD_PARTY_TEXT) || text.includes(API_TEXT)) return false;
  return [
    '\u4e0b\u8f7d Codex',
    '\u4e0b\u8f7dCodex',
    'Codex \u6700\u65b0\u5ba2\u6237\u7aef',
    '\u7acb\u5373\u4e0b\u8f7d Codex',
    '\u7acb\u5373\u4e0b\u8f7d Codex \u8d44\u6e90',
    '\u4e0b\u8f7d\u5e76\u5f00\u59cb\u4f53\u9a8c',
    '\u4e0b\u8f7d macOS',
    '\u4e0b\u8f7d Windows',
    '\u4e0b\u8f7d Visual Studio Code',
    '\u4e0b\u8f7d Cursor',
    '\u4e0b\u8f7d Windsurf',
    '\u4e0b\u8f7d VS Code Insiders',
    '\u5b89\u88c5 Codex for JetBrains IDEs',
    'Visual Studio Code',
    'Cursor',
    'Windsurf',
    'VS Code Insiders',
  ].some((needle) => text.includes(needle));
}

function restoreCodexDownloadTargets(input) {
  return input
    .replace(/"downloadUrl":\s*"https:\/\/ai\.icodett\.xyz\/?"/g, `"downloadUrl": "${QUARK_VISIBLE_SHARE_URL}"`)
    .replace(/<a\b([^>]*?)href="(?:https:\/\/ai\.icodett\.xyz(?:#\/list\/share)?\/?|https:\/\/pan\.quark\.cn\/s\/[a-zA-Z0-9]+)"([^>]*)>([\s\S]*?)<\/a>/g, (match, before, after, body) => {
      if (!isCodexDownloadAnchor(anchorText(body))) return match;
      return `<a${before}href="${QUARK_VISIBLE_SHARE_URL}"${after}>${body}</a>`;
    });
}

function rewriteDownloadLinks(input) {
  let output = input
    .replaceAll(`${OLD_PUBLIC_HOST}/`, `${PUBLIC_HOST}/`)
    .replaceAll(OLD_PUBLIC_HOST, PUBLIC_HOST)
    .replaceAll(`http://${OLD_HOSTNAME}/`, `${PUBLIC_HOST}/`)
    .replaceAll(`http://${OLD_HOSTNAME}`, PUBLIC_HOST)
    .replaceAll(OLD_HOSTNAME, PUBLIC_HOSTNAME)
    .replaceAll(`${OLD_PROXY_HOST}/`, `${PROXY_HOST}/`)
    .replaceAll(OLD_PROXY_HOST, PROXY_HOST)
    .replaceAll(`http://${OLD_PROXY_HOSTNAME}/`, `${PROXY_HOST}/`)
    .replaceAll(`http://${OLD_PROXY_HOSTNAME}`, PROXY_HOST)
    .replaceAll(OLD_PROXY_HOSTNAME, PROXY_HOSTNAME)
    .replaceAll(QUARK_SHARE_URL, PROXY_HOST)
    .replaceAll(QUARK_SHARE_URL.replace('https://', 'http://'), PROXY_HOST)
    .replaceAll(`${QUARK_HOSTNAME}/s/${QUARK_SHARE_ID}`, PROXY_HOSTNAME);
  for (const legacyId of LEGACY_QUARK_SHARE_IDS) {
    const legacyPath = `${QUARK_HOSTNAME}/s/${legacyId}`;
    output = output
      .replaceAll(`https://${legacyPath}`, QUARK_VISIBLE_SHARE_URL)
      .replaceAll(`http://${legacyPath}`, QUARK_VISIBLE_SHARE_URL)
      .replaceAll(legacyPath, QUARK_VISIBLE_SHARE_PATH);
  }
  return restoreCodexDownloadTargets(preserveVisibleQuarkShare(output));
}

async function rewriteAllTextFiles() {
  const exts = new Set(['.html', '.xml', '.txt', '.json', '.js', '.css', '.href', '.bin']);
  let changed = 0;
  for await (const file of walk(path.join(ROOT, 'public/mirror'))) {
    if (!exts.has(path.extname(file))) continue;
    const bytes = await fs.readFile(file);
    if (bytes.includes(0)) continue;
    const old = bytes.toString('utf8');
    const next = rewriteDownloadLinks(old);
    if (next !== old) {
      await fs.writeFile(file, next);
      changed += 1;
    }
  }
  return changed;
}

async function patchHomepage() {
  let html = await fs.readFile(homepagePath, 'utf8');
  html = rewriteDownloadLinks(html);
  html = html.replace(
    /(<a class="proxy-promo-card[^"]*"[^>]*\s)href="[^"]+"/,
    `$1href="${PROXY_HOST}"`,
  );

  const premiumCss = `
      /* Yancc premium visual pass: calmer editorial-tech background, no cheap blue wash. */
      html {
        background:
          radial-gradient(70% 52% at 18% 8%, rgba(255,255,255,.92) 0%, rgba(236,241,255,.70) 30%, rgba(236,241,255,0) 68%),
          radial-gradient(56% 48% at 82% 14%, rgba(155,170,218,.42) 0%, rgba(155,170,218,.16) 38%, rgba(155,170,218,0) 72%),
          radial-gradient(60% 56% at 72% 88%, rgba(196,170,128,.24) 0%, rgba(196,170,128,.08) 40%, rgba(196,170,128,0) 76%),
          linear-gradient(135deg, #f8f7f2 0%, #edf1f7 45%, #dfe6f1 100%) !important;
      }

      body {
        color: #141821 !important;
        background: transparent !important;
      }

      body::before {
        opacity: .12 !important;
        mix-blend-mode: multiply !important;
      }

      body::after {
        background:
          linear-gradient(180deg, rgba(255,255,255,.34) 0%, rgba(255,255,255,.08) 46%, rgba(24,27,35,.04) 100%),
          radial-gradient(92% 80% at 50% 8%, rgba(255,255,255,.84) 0%, rgba(255,255,255,0) 72%),
          radial-gradient(92% 90% at 50% 54%, rgba(255,255,255,0) 58%, rgba(33,39,56,.10) 100%) !important;
      }

      .site-shell::before {
        background: radial-gradient(circle at 50% 50%, rgba(255,255,255,.72) 0%, rgba(217,225,238,.24) 34%, rgba(255,255,255,0) 74%) !important;
        filter: blur(24px) !important;
      }

      .site-shell::after {
        background: radial-gradient(circle at 50% 50%, rgba(152,132,100,.22) 0%, rgba(152,132,100,.10) 38%, rgba(152,132,100,0) 76%) !important;
        filter: blur(34px) !important;
      }

      .hero-surface {
        border: 1px solid rgba(255,255,255,.72) !important;
        background:
          linear-gradient(135deg, rgba(255,255,255,.92), rgba(247,249,252,.74) 46%, rgba(229,235,246,.66)),
          radial-gradient(80% 70% at 16% 12%, rgba(255,255,255,.96), transparent 66%) !important;
        box-shadow:
          0 38px 120px rgba(49,58,82,.18),
          inset 0 1px 0 rgba(255,255,255,.95),
          inset 0 -1px 0 rgba(73,84,110,.08) !important;
      }

      .hero-surface > [aria-hidden="true"][style*="radial-gradient"] {
        opacity: .66 !important;
        background:
          radial-gradient(42% 38% at 18% 24%, rgba(255,255,255,.90) 0%, rgba(255,255,255,.28) 38%, rgba(255,255,255,0) 74%),
          radial-gradient(36% 42% at 76% 18%, rgba(123,143,192,.30) 0%, rgba(123,143,192,.12) 44%, rgba(123,143,192,0) 78%),
          radial-gradient(32% 34% at 82% 72%, rgba(186,157,112,.20) 0%, rgba(186,157,112,.08) 42%, rgba(186,157,112,0) 78%) !important;
      }

      .hero-surface h1,
      .hero-surface h2,
      .hero-surface h3,
      .hero-surface p,
      .hero-surface span:not(.hero-mark):not(.hero-mark *) {
        color: #111722;
      }

      .hero-surface .rounded-full,
      header .rounded-full {
        box-shadow: 0 16px 44px rgba(35,42,58,.10);
      }

      .hero-surface a[href="${PROXY_HOST}"] {
        background: #121722 !important;
        color: #fff !important;
        border-color: rgba(255,255,255,.24) !important;
        box-shadow: 0 22px 64px rgba(18,23,34,.22) !important;
      }
  `;

  if (!html.includes('Yancc premium visual pass')) {
    html = html.replace('</style>', `${premiumCss}\n    </style>`);
  }
  await fs.writeFile(homepagePath, html);
}

async function patchPetsData() {
  let js = await fs.readFile(petsDataPath, 'utf8');
  js = rewriteDownloadLinks(js);
  js = js.replaceAll('"image": "/assets/images/pets/', '"image": "/assets/images/pets/');
  await fs.writeFile(petsDataPath, js);
}

async function patchPetsScript() {
  let js = await fs.readFile(petsScriptPath, 'utf8');
  js = rewriteDownloadLinks(js);
  if (!js.includes('const assetUrl = (value) =>')) {
    js = js.replace(
      '  const escapeHtml = (value) => String(value)\n    .replaceAll("&", "&amp;")\n    .replaceAll("<", "&lt;")\n    .replaceAll(">", "&gt;")\n    .replaceAll(\'"\', "&quot;")\n    .replaceAll("\'", "&#039;");\n',
      '  const escapeHtml = (value) => String(value)\n    .replaceAll("&", "&amp;")\n    .replaceAll("<", "&lt;")\n    .replaceAll(">", "&gt;")\n    .replaceAll(\'"\', "&quot;")\n    .replaceAll("\'", "&#039;");\n\n  const assetUrl = (value) => {\n    const url = String(value || "");\n    if (url.startsWith("/assets/images/pets/")) return url;\n    if (url.startsWith("https://codexdown.cn/assets/images/pets/")) return url.replace("https://codexdown.cn", "");\n    return url;\n  };\n'
    );
  }
  js = js.replace(
    "return `<span class=\"pet-sprite\" style=\"background-image: url('${escapeHtml(pet.image)}'); background-position: 0 ${y}px;\" aria-hidden=\"true\"></span>`;",
    "return `<span class=\"pet-sprite\" style=\"background-image: url('${escapeHtml(assetUrl(pet.image))}'); background-position: 0 ${y}px;\" aria-hidden=\"true\"></span>`;"
  );
  await fs.writeFile(petsScriptPath, js);
}

async function patchPetsCss() {
  let css = await fs.readFile(petsCssPath, 'utf8');
  if (!css.includes('Yancc pets preview fix')) {
    css += `

/* Yancc pets preview fix */
.pet-sprite {
  background-repeat: no-repeat;
  background-size: 208px auto;
  image-rendering: auto;
  transform: translateZ(0);
}
.pet-card-media .pet-sprite {
  filter: drop-shadow(0 18px 28px rgba(15, 23, 42, .18));
}
.pet-card {
  min-height: 100%;
}
`;
  }
  if (!css.includes('Yancc pets header contrast fix')) {
    css += `

/* Yancc pets header contrast fix */
.site-header:not(.is-scrolled) .site-brand {
  color: #ffffff;
  text-shadow: 0 2px 18px rgba(0, 0, 0, .34);
}

.site-header:not(.is-scrolled) .site-nav-link {
  color: rgba(255, 255, 255, .82);
  text-shadow: 0 2px 18px rgba(0, 0, 0, .28);
}

.site-header:not(.is-scrolled) .site-nav-link:hover,
.site-header:not(.is-scrolled) .site-nav-link[aria-current="page"] {
  color: #ffffff;
}

.site-header:not(.is-scrolled) .site-nav-link::after {
  background: #ffffff;
}

.site-header:not(.is-scrolled) .site-brand-icon {
  border-color: rgba(255, 255, 255, .42);
  background: rgba(255, 255, 255, .82);
}
`;
  }
  await fs.writeFile(petsCssPath, css);
}

const changed = await rewriteAllTextFiles();
await patchHomepage();
await patchPetsData();
await patchPetsScript();
await patchPetsCss();
console.log(JSON.stringify({ changedTextFiles: changed }, null, 2));
