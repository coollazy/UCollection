// Cross-checked against internal/hdwallet's Go test vectors
// (internal/hdwallet/hdwallet_test.go) and 驗證結論-01/04's known addresses —
// same mnemonic, same xpub, same index-0~4 addresses, so a mismatch here
// means the JS derivation path has diverged from the already-shipped Go
// implementation, not just "the JS test is wrong".
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { deriveConsolidationKey, deriveFeeTopupKeyFromMnemonic, feeTopupKeyFromPrivateKeyHex } from './derive.js';

const TEST_MNEMONIC = 'abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';
const WANT_XPUB = 'xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd';
const WANT_ADDRESSES = {
  0: 'TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH',
  1: 'TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK',
  2: 'TYJPRrdB5APNeRs4R7fYZSwW3TcrTKw2gx',
  3: 'TRhVWK5XEDkQBDevcdCWW7RW51aRncty4W',
  4: 'TT2X2yyubp7qpAWYYNE5JQWBtoZ7ikQFsY',
};

test('deriveConsolidationKey matches known Go/驗證結論-01 vectors for index 0-4', () => {
  for (const [indexStr, wantAddress] of Object.entries(WANT_ADDRESSES)) {
    const index = Number(indexStr);
    const got = deriveConsolidationKey(TEST_MNEMONIC, index);
    assert.equal(got.xpub, WANT_XPUB, `xpub mismatch at index ${index}`);
    assert.equal(got.address, wantAddress, `address mismatch at index ${index}`);
    assert.equal(got.privateKey.length, 32);
  }
});

test('deriveConsolidationKey rejects a malformed mnemonic', () => {
  assert.throws(() => deriveConsolidationKey('not a real bip39 mnemonic phrase', 0), /助記詞格式不正確/);
});

test('deriveConsolidationKey rejects a negative or non-integer index', () => {
  assert.throws(() => deriveConsolidationKey(TEST_MNEMONIC, -1), /index/);
  assert.throws(() => deriveConsolidationKey(TEST_MNEMONIC, 1.5), /index/);
});

test('deriveFeeTopupKeyFromMnemonic always uses index 0 of its own account, independent of the master wallet tree', () => {
  const got = deriveFeeTopupKeyFromMnemonic(TEST_MNEMONIC);
  assert.equal(got.address, WANT_ADDRESSES[0]);
});

test('feeTopupKeyFromPrivateKeyHex and deriveConsolidationKey agree on the same underlying key', () => {
  const viaMnemonic = deriveConsolidationKey(TEST_MNEMONIC, 0);
  const hex = Buffer.from(viaMnemonic.privateKey).toString('hex');
  const viaPrivateKey = feeTopupKeyFromPrivateKeyHex(hex);
  assert.equal(viaPrivateKey.address, viaMnemonic.address);
  // Buffer.from(...).toString('hex') round-trip is a byte-content check —
  // deepEqual would also compare Uint8Array vs Buffer prototypes, which
  // differ even when the underlying bytes are identical.
  assert.equal(Buffer.from(viaPrivateKey.privateKey).toString('hex'), Buffer.from(viaMnemonic.privateKey).toString('hex'));
});

test('feeTopupKeyFromPrivateKeyHex rejects malformed hex', () => {
  assert.throws(() => feeTopupKeyFromPrivateKeyHex('not hex'), /私鑰格式不正確/);
  assert.throws(() => feeTopupKeyFromPrivateKeyHex('ab'.repeat(31)), /私鑰格式不正確/); // 62 chars, too short
});

test('feeTopupKeyFromPrivateKeyHex rejects an all-zero key (outside the valid curve range)', () => {
  assert.throws(() => feeTopupKeyFromPrivateKeyHex('00'.repeat(32)), /私鑰數值超出secp256k1合法範圍/);
});

test('feeTopupKeyFromPrivateKeyHex rejects a key at/above the curve order n', () => {
  // secp256k1 order n = FFFFFFFF FFFFFFFF FFFFFFFF FFFFFFFE BAAEDCE6 AF48A03B BFD25E8C D0364141
  const atOrder = 'FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141';
  assert.throws(() => feeTopupKeyFromPrivateKeyHex(atOrder), /私鑰數值超出secp256k1合法範圍/);
});
