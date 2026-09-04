// Post-signature self-check: reverse a signature back to the address that
// produced it, and require it to equal the address we expected to sign
// with — CLAUDE.md安全鐵律4「簽名後、廣播前必做...反推地址自我核對」與技術架構設計第10節
// 步驟4, cross-validated in 驗證結論-06/08. This is the last line of defense
// before anything gets sent to /tron-proxy/.../broadcast; a false result
// here must abort the whole item, not just log a warning.
import { secp256k1 } from '@noble/curves/secp256k1.js';
import { hexToBytes } from '@noble/hashes/utils.js';
import { publicKeyToTronAddress } from './tronaddress.js';

// verifySignerAddress takes the transaction sign.js just produced (its
// signature[0] is 130 hex chars: r(32) || s(32) || v(1), v = recovery + 27
// — see sign.js) and the txID it was signed over, recovers the signer's
// public key, and compares the resulting Tron address against
// expectedAddress. Returns a plain boolean — callers decide what "false"
// means for their flow (abort, show an error, never broadcast).
export function verifySignerAddress(signedTransaction, expectedAddress) {
  if (!signedTransaction || typeof signedTransaction.txID !== 'string') {
    throw new Error('verifySignerAddress: signedTransaction is missing txID');
  }
  const sigHex = signedTransaction.signature && signedTransaction.signature[0];
  if (typeof sigHex !== 'string' || sigHex.length !== 130) {
    throw new Error('verifySignerAddress: signedTransaction.signature[0] must be a 130-char hex string (r||s||v)');
  }

  const rsv = hexToBytes(sigHex);
  const r = rsv.subarray(0, 32);
  const s = rsv.subarray(32, 64);
  const recovery = rsv[64] - 27;

  // Rebuild the 'recovered' format @noble/curves expects for recovery:
  // recovery(1) || r(32) || s(32) — the inverse of sign.js's rearrangement.
  const recoveredFormat = new Uint8Array(65);
  recoveredFormat[0] = recovery;
  recoveredFormat.set(r, 1);
  recoveredFormat.set(s, 33);

  const txIDBytes = hexToBytes(signedTransaction.txID);
  const signerPoint = secp256k1.Signature.fromBytes(recoveredFormat, 'recovered').recoverPublicKey(txIDBytes);
  const uncompressedPubKey = signerPoint.toBytes(false);
  const recoveredAddress = publicKeyToTronAddress(uncompressedPubKey);

  return recoveredAddress === expectedAddress;
}
