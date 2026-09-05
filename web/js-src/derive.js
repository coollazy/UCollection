// Mnemonic/private-key -> private key derivation for the manual
// consolidation sign page. Runs entirely in the browser tab's memory
// (CLAUDE.md 安全鐵律1) — nothing here ever leaves this module's return
// values to the network. Library choice and derivation path are pinned by
// 技術架構設計第10節「助記詞輸入與私鑰衍生」 (bip39 + hdkey, m/44'/195'/0'/0/{index}),
// already cross-validated against Go's internal/hdwallet output in
// 驗證結論-01/03/04 — this file does not invent new derivation logic.
import { mnemonicToSeedSync, validateMnemonic } from 'bip39';
import HDKey from 'hdkey';
import { secp256k1 } from '@noble/curves/secp256k1.js';
import { hexToBytes } from '@noble/hashes/utils.js';
import { publicKeyToTronAddress } from './tronaddress.js';

const ACCOUNT_PATH = "m/44'/195'/0'";

function addressFromPrivateKey(privateKey) {
  const uncompressedPubKey = secp256k1.getPublicKey(privateKey, false);
  return publicKeyToTronAddress(uncompressedPubKey);
}

// seedFromValidatedMnemonic validates+normalizes the mnemonic and returns
// its BIP39 seed. Shared by both consolidation and fee-topup mnemonic
// paths so the "bad mnemonic" error is worded identically everywhere.
function seedFromValidatedMnemonic(mnemonic) {
  const trimmed = typeof mnemonic === 'string' ? mnemonic.trim() : '';
  if (!validateMnemonic(trimmed)) {
    throw new Error('助記詞格式不正確，請重新輸入');
  }
  return mnemonicToSeedSync(trimmed);
}

// deriveConsolidationKey derives the child key for one consolidation item
// at its own derivation_index, plus the account-level xpub for the caller
// to cross-check against master_wallets.xpub (技術架構設計第10節「與該代收主
// 錢包已存的 master_wallets.xpub...逐字比對，不符則拒絕」) BEFORE trusting any
// derived private key. Hardened derivation (m/44'/195'/0') can only be done
// from the mnemonic/seed, never from an xpub — this is a BIP32 property,
// not an implementation choice.
export function deriveConsolidationKey(mnemonic, index) {
  if (!Number.isInteger(index) || index < 0) {
    throw new Error('deriveConsolidationKey: index must be a non-negative integer');
  }
  const seed = seedFromValidatedMnemonic(mnemonic);
  const account = HDKey.fromMasterSeed(seed).derive(ACCOUNT_PATH);
  const child = account.deriveChild(0).deriveChild(index);
  return {
    privateKey: child.privateKey,
    xpub: account.publicExtendedKey,
    address: addressFromPrivateKey(child.privateKey),
  };
}

// deriveAccountXpub derives only the account-level xpub (m/44'/195'/0'),
// for /admin/master-wallets/new (技術架構設計第11節「新增」：「本頁僅需衍生到xpub
// 層級...不需要@noble/curves等簽名相關函式庫」). Deliberately narrower than
// deriveConsolidationKey: this page never needs a child private key at all,
// so it never derives one — no reason to materialize key material in
// memory that this page has no use for.
export function deriveAccountXpub(mnemonic) {
  const seed = seedFromValidatedMnemonic(mnemonic);
  const account = HDKey.fromMasterSeed(seed).derive(ACCOUNT_PATH);
  return account.publicExtendedKey;
}

// deriveFeeTopupKeyFromMnemonic is deriveConsolidationKey's counterpart for
// the A1/A2 TRX fee-topup flow (技術架構設計第10節「TRX 手續費」): the source
// address is 商戶自行指定的任意地址, unrelated to the master wallet's HD tree,
// so the path is always fixed at index 0 of THIS mnemonic's own account —
// there is no xpub to cross-check against (fee-topup mode's
// signPageJSON.xpub is always empty).
export function deriveFeeTopupKeyFromMnemonic(mnemonic) {
  const seed = seedFromValidatedMnemonic(mnemonic);
  const child = HDKey.fromMasterSeed(seed).derive(ACCOUNT_PATH + "/0/0");
  return { privateKey: child.privateKey, address: addressFromPrivateKey(child.privateKey) };
}

const HEX64_RE = /^[0-9a-fA-F]{64}$/;

// feeTopupKeyFromPrivateKeyHex is the other A1/A2 input mode: a raw private
// key hex string (技術架構設計第10節「直接輸入私鑰 hex」). Format check (64 hex
// chars) then curve-range check (secp256k1.utils.isValidSecretKey, i.e.
// 0 < key < curve order) — an out-of-range or all-zero key must be
// rejected before any address is derived from it, same spirit as the
// mnemonic path's format validation.
export function feeTopupKeyFromPrivateKeyHex(hex) {
  const trimmed = typeof hex === 'string' ? hex.trim() : '';
  if (!HEX64_RE.test(trimmed)) {
    throw new Error('私鑰格式不正確，需為64碼十六進位字元');
  }
  const privateKey = hexToBytes(trimmed);
  if (!secp256k1.utils.isValidSecretKey(privateKey)) {
    throw new Error('私鑰數值超出secp256k1合法範圍');
  }
  return { privateKey, address: addressFromPrivateKey(privateKey) };
}
