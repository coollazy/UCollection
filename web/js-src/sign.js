// Signs an unsigned transaction returned by POST /tron-proxy/.../prepare.
// This is the step CLAUDE.md安全鐵律4 and 技術架構設計第10節「組交易與廣播」步驟2/3
// describe: recompute txID locally before ever signing (驗證結論-06/08's
// required step, not optional), then sign with the exact byte layout
// TronGrid's broadcasttransaction expects. Every quirk noted below was a
// real, silent-failure pitfall hit during 驗證結論-06 (see that document's
// "過程中的觀察" section) — this file exists specifically to not re-hit them.
import { secp256k1 } from '@noble/curves/secp256k1.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { hexToBytes, bytesToHex } from '@noble/hashes/utils.js';

// signTransaction recomputes sha256(raw_data_hex) and compares it against
// the transaction's own txID field FIRST. Only if that matches does it
// proceed to sign — a mismatch here means the transaction object was
// tampered with or corrupted in transit, and signing it anyway (even if
// the signature would be technically valid) is never correct, so this
// function refuses to produce a signature in that case.
export function signTransaction(transaction, privateKey) {
  if (!transaction || typeof transaction.raw_data_hex !== 'string' || typeof transaction.txID !== 'string') {
    throw new Error('signTransaction: transaction is missing raw_data_hex/txID');
  }

  const recomputedTxID = bytesToHex(sha256(hexToBytes(transaction.raw_data_hex)));
  if (recomputedTxID !== transaction.txID) {
    return { signedTransaction: null, txIDMatched: false };
  }

  const txIDBytes = hexToBytes(transaction.txID);

  // { prehash: false } is mandatory: transaction.txID is ALREADY
  // sha256(raw_data) — @noble/curves' default (prehash: true) would hash it
  // a second time and sign SHA256(SHA256(raw_data)) instead, which is a
  // functioning call that produces a wrong, useless signature (驗證結論-06
  // 觀察3). { format: 'recovered' } is what makes the recovery id available
  // at all — without it, sign() returns a plain compact signature with no
  // way to recover the signer's public key later for the selfcheck step.
  const sig = secp256k1.sign(txIDBytes, privateKey, { prehash: false, format: 'recovered' });

  // sig layout is recovery(1) || r(32) || s(32) — recovery FIRST, not last
  // (驗證結論-06 觀察2: assuming the tronweb-style r||s||v layout here
  // silently produces a 65-byte value that looks like a valid signature but
  // gets rejected on-chain with SIGERROR). TronGrid/tronweb want
  // r(32) || s(32) || v(1), v = recovery + 27.
  const recovery = sig[0];
  const r = sig.subarray(1, 33);
  const s = sig.subarray(33, 65);
  const rsv = new Uint8Array(65);
  rsv.set(r, 0);
  rsv.set(s, 32);
  rsv[64] = recovery + 27;

  const signedTransaction = { ...transaction, signature: [bytesToHex(rsv)] };
  return { signedTransaction, txIDMatched: true };
}
