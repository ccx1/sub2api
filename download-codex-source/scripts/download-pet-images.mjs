import { mkdir, readFile, writeFile, readdir, access } from 'node:fs/promises';
import path from 'node:path';

const ROOT = process.cwd();
const ASSETS = path.join(ROOT, 'public', 'mirror', 'assets');
const META = path.join(ROOT, 'docs', 'research', 'codexdown');
const CONCURRENCY = Number.parseInt(process.env.PET_DOWNLOAD_CONCURRENCY || '16', 10);
const STRICT = process.env.PET_DOWNLOAD_STRICT === '1';
const ua = 'Mozilla/5.0 (compatible; HermesClone/1.0; +https://codex.download.icodett.xyz)';

async function exists(file) {
  try {
    await access(file);
    return true;
  } catch {
    return false;
  }
}

async function ensureDir(dir) {
  await mkdir(dir, { recursive: true });
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function fetchBuffer(url, attempt = 1) {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 45000);
  try {
    const res = await fetch(url, {
      headers: { 'user-agent': ua },
      signal: ctrl.signal,
      redirect: 'follow',
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return Buffer.from(await res.arrayBuffer());
  } catch (error) {
    if (attempt < 4) {
      await sleep(1000 * attempt);
      return fetchBuffer(url, attempt + 1);
    }
    throw error;
  } finally {
    clearTimeout(timer);
  }
}

async function loadPets() {
  const files = await readdir(ASSETS);
  const dataFile = files.find((file) => /^pets-data-.*\.js$/.test(file));
  if (!dataFile) throw new Error(`pets-data asset not found in ${ASSETS}`);

  const js = await readFile(path.join(ASSETS, dataFile), 'utf8');
  const match = js.match(/window\.CODEX_PETS\s*=\s*(\[[\s\S]*\])\s*;/);
  if (!match) throw new Error(`Unable to parse ${dataFile}`);
  return JSON.parse(match[1]);
}

function localPathFor(pet) {
  const image = String(pet.image || '');
  const normalized = image.startsWith('/') ? image.slice(1) : image;
  if (!normalized.startsWith('assets/images/pets/')) {
    throw new Error(`Unexpected pet image path for ${pet.id}: ${image}`);
  }
  return path.join(ROOT, 'public', normalized);
}

async function main() {
  const pets = await loadPets();
  const results = [];
  let cursor = 0;
  let ok = 0;
  let skipped = 0;
  let failed = 0;

  async function worker() {
    while (cursor < pets.length) {
      const pet = pets[cursor++];
      const source = pet.source?.spritesheetUrl;
      const dest = localPathFor(pet);
      try {
        if (!source) throw new Error('missing spritesheetUrl');
        if (await exists(dest)) {
          skipped++;
          results.push({ id: pet.id, status: 'skipped', dest: path.relative(ROOT, dest) });
          continue;
        }
        const body = await fetchBuffer(source);
        await ensureDir(path.dirname(dest));
        await writeFile(dest, body);
        ok++;
        results.push({ id: pet.id, status: 'ok', bytes: body.length, source, dest: path.relative(ROOT, dest) });
      } catch (error) {
        failed++;
        results.push({ id: pet.id, status: 'error', error: String(error), source, dest: path.relative(ROOT, dest) });
        console.warn('pet failed', pet.id, String(error));
      }

      const done = ok + skipped + failed;
      if (done % 100 === 0 || done === pets.length) {
        console.log('pets', done, '/', pets.length, 'ok', ok, 'skipped', skipped, 'failed', failed);
      }
    }
  }

  await Promise.all(Array.from({ length: CONCURRENCY }, worker));
  await ensureDir(META);
  await writeFile(
    path.join(META, 'pet-image-download.json'),
    JSON.stringify(
      {
        count: pets.length,
        counts: { ok, skipped, failed },
        failed: results.filter((item) => item.status === 'error'),
      },
      null,
      2,
    ),
  );

  if (failed > 0 && STRICT) process.exitCode = 1;
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
