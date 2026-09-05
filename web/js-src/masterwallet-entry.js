// Entry point for master_wallet_new.html — mirrors sign-entry.js's role for
// consolidation_sign.html. Deliberately just wiring, no logic of its own.
import { initMasterWalletPage } from './masterwallet-page.js';

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', initMasterWalletPage);
} else {
  initMasterWalletPage();
}
