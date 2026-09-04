// Builds the two script files consolidation_sign.html loads, in the order
// it loads them: polyfill.bundle.js (Buffer/process globals — must finish
// running before bundle.js's module graph evaluates, see
// web/js-src/polyfill-entry.js) then bundle.js (derive/sign/selfcheck/
// storage/page, entered via sign-entry.js). Same esbuild version and
// --platform=browser --bundle approach as 驗證結論-04/05/06/08; the alias
// list below is this project's own dependency tree resolved from scratch
// against esbuild's actual "could not resolve" errors, not copied from the
// verification docs' older library versions.
//
// Usage: node scripts/build-consolidation-sign.mjs [--check]
//   --check: build into a temp dir and diff against the committed output
//   instead of overwriting it — used by CI to catch a source change that
//   was committed without re-running this script.
import { build } from 'esbuild';
import { readFileSync, mkdtempSync, rmSync, existsSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const outDir = path.join(repoRoot, 'web', 'static', 'js');

// hdkey's dependency tree (bs58check/secp256k1/hash-base/browserify-sign/
// crypto-browserify/...) assumes Node core modules exist. bip39 3.1.0 no
// longer needs any of these (it uses @noble/hashes internally) but hdkey
// still does — this alias list is exactly what building against
// web/js-src/sign-entry.js actually required, found by iterating on
// esbuild's "Could not resolve" errors one at a time, not assumed upfront.
const NODE_BUILTIN_ALIASES = {
  crypto: 'crypto-browserify',
  stream: 'stream-browserify',
  assert: 'assert',
  buffer: 'buffer',
  process: 'process',
  events: 'events',
};

const targets = [
  { entry: 'web/js-src/polyfill-entry.js', out: 'polyfill.bundle.js' },
  { entry: 'web/js-src/sign-entry.js', out: 'bundle.js' },
];

async function buildInto(destDir) {
  mkdirSync(destDir, { recursive: true });
  for (const t of targets) {
    await build({
      entryPoints: [path.join(repoRoot, t.entry)],
      outfile: path.join(destDir, t.out),
      bundle: true,
      platform: 'browser',
      format: 'iife',
      alias: NODE_BUILTIN_ALIASES,
      // crypto-browserify's dependency tree (pulled in via hdkey's
      // require('crypto') -> our crypto-browserify alias) assumes a
      // bundler that shims the bare Node `global` identifier to `window`
      // (webpack/browserify do this automatically; esbuild does not) —
      // without this, randombytes/browser.js's top-level
      // `global.crypto || global.msCrypto` throws ReferenceError in any
      // real browser, not just under test — found by actually running the
      // built bundle (web/js-src/bundle.smoketest.js), not by inspection.
      define: { global: 'window' },
      logLevel: 'warning',
    });
  }
}

const checkMode = process.argv.includes('--check');

if (!checkMode) {
  await buildInto(outDir);
  console.log('built:', targets.map((t) => t.out).join(', '), '->', path.relative(repoRoot, outDir));
} else {
  const tmp = mkdtempSync(path.join(tmpdir(), 'ucollection-sign-bundle-'));
  try {
    await buildInto(tmp);
    let drifted = false;
    for (const t of targets) {
      const committedPath = path.join(outDir, t.out);
      const freshPath = path.join(tmp, t.out);
      if (!existsSync(committedPath)) {
        console.error(`missing committed file: ${path.relative(repoRoot, committedPath)} (run: node scripts/build-consolidation-sign.mjs)`);
        drifted = true;
        continue;
      }
      const committed = readFileSync(committedPath, 'utf8');
      const fresh = readFileSync(freshPath, 'utf8');
      if (committed !== fresh) {
        console.error(`drift detected in ${t.out} — committed bundle does not match a fresh build from web/js-src/. Run: node scripts/build-consolidation-sign.mjs`);
        drifted = true;
      }
    }
    if (drifted) process.exit(1);
    console.log('bundle drift check passed: committed web/static/js/*.bundle output matches web/js-src/ source');
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
}
