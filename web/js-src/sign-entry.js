// Entry point for consolidation_sign.html — this is what bundle.js
// actually runs; everything else (derive/sign/selfcheck/storage/page) is
// pulled in as imports and bundled together by esbuild. Deliberately just
// wiring, no logic of its own.
import { initSignPage } from './page.js';

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', initSignPage);
} else {
  initSignPage();
}
