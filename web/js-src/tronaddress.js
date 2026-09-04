// Tron Base58Check address encoding from a public key — the JS mirror of
// internal/hdwallet's Go logic (Keccak256(uncompressed pubkey, no 0x04
// prefix) -> last 20 bytes -> 0x41 prefix -> Base58Check). Cross-validated
// against 驗證結論-01/03/04's known vectors (see *.test.js).
import { keccak_256 } from '@noble/hashes/sha3.js';
import bs58check from 'bs58check';

const TRON_ADDRESS_PREFIX = 0x41;

// publicKeyToTronAddress takes an UNCOMPRESSED secp256k1 public key
// (65 bytes, 0x04 prefix, as returned by @noble/curves' getPublicKey(...,
// false) or Signature.recoverPublicKey(...).toBytes(false)) and returns the
// Tron Base58Check address string ("T..."). bs58check.encode() computes and
// appends the double-SHA256 checksum internally — callers must not
// checksum the payload themselves.
export function publicKeyToTronAddress(uncompressedPubKey) {
  if (!(uncompressedPubKey instanceof Uint8Array) || uncompressedPubKey.length !== 65 || uncompressedPubKey[0] !== 0x04) {
    throw new Error('publicKeyToTronAddress: expected a 65-byte uncompressed public key (0x04 prefix)');
  }
  const xy = uncompressedPubKey.subarray(1); // drop the 0x04 prefix, 64 bytes
  const hash = keccak_256(xy);
  const last20 = hash.subarray(hash.length - 20);
  const payload = new Uint8Array(21);
  payload[0] = TRON_ADDRESS_PREFIX;
  payload.set(last20, 1);
  return bs58check.encode(payload);
}
