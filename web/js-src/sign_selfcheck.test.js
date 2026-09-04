// sign.js/selfcheck.js round-trip tests. The unsigned-transaction fixture
// below is a real TronGrid triggersmartcontract response captured during
// this module's development (see internal/tronclient/transaction_test.go's
// triggerSmartContractSuccessFixture) — using a real fixture, not a
// hand-rolled one, means the raw_data_hex/txID relationship being tested
// is the one TronGrid actually produces, not an assumption about its shape.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { deriveConsolidationKey } from './derive.js';
import { signTransaction } from './sign.js';
import { verifySignerAddress } from './selfcheck.js';

const TEST_MNEMONIC = 'abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';

// Same fixture as internal/tronclient's triggerSmartContractSuccessFixture
// (owner TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH == index 0 of TEST_MNEMONIC).
const FIXTURE_TRANSACTION = {
  raw_data: {
    ref_block_bytes: 'e5e0',
    ref_block_hash: '2c5c36c5fea2de8a',
    expiration: 1788506022000,
    contract: [{ type: 'TriggerSmartContract' }],
    timestamp: 1788505964581,
    fee_limit: 100000000,
  },
  raw_data_hex:
    '0a02e5e022082c5c36c5fea2de8a40f0c090da86345aae01081f12a9010a31747970652e676f6f676c65617069732e636f6d2f70726f746f636f6c2e54726967676572536d617274436f6e747261637412740a1541c8599111f29c1e1e061265b4af93ea1f274ad78a12154142a1e39aefa49290f2b3f9ed688d7cecf86cd6e02244a9059cbb000000000000000000000000b6e708a39781c96bd399c7657780ff9fe9f052a8000000000000000000000000000000000000000000000000000000000012d68770a5808dda8634900180c2d72f',
  txID: 'e100b697f91c4c5067ffdbc5fdcc46bdd7f3eae4cf2aba61841b120766fa370c',
  visible: true,
};

test('signTransaction recomputes and matches the real TronGrid txID', () => {
  const key0 = deriveConsolidationKey(TEST_MNEMONIC, 0);
  const { signedTransaction, txIDMatched } = signTransaction(FIXTURE_TRANSACTION, key0.privateKey);
  assert.equal(txIDMatched, true);
  assert.equal(signedTransaction.signature.length, 1);
  assert.equal(signedTransaction.signature[0].length, 130); // r(32)+s(32)+v(1) hex
  // every other field from the original transaction must survive untouched —
  // broadcast.go's extractTxIDFromJSON depends on txID still being present.
  assert.equal(signedTransaction.txID, FIXTURE_TRANSACTION.txID);
  assert.equal(signedTransaction.raw_data_hex, FIXTURE_TRANSACTION.raw_data_hex);
  assert.equal(signedTransaction.visible, true);
});

test('signTransaction refuses to sign when the transaction has been tampered with', () => {
  const key0 = deriveConsolidationKey(TEST_MNEMONIC, 0);
  const tampered = { ...FIXTURE_TRANSACTION, txID: 'deadbeef' + FIXTURE_TRANSACTION.txID.slice(8) };
  const { signedTransaction, txIDMatched } = signTransaction(tampered, key0.privateKey);
  assert.equal(txIDMatched, false);
  assert.equal(signedTransaction, null);
});

test('verifySignerAddress returns true for the correct signer', () => {
  const key0 = deriveConsolidationKey(TEST_MNEMONIC, 0);
  const { signedTransaction } = signTransaction(FIXTURE_TRANSACTION, key0.privateKey);
  assert.equal(verifySignerAddress(signedTransaction, key0.address), true);
});

test('verifySignerAddress returns false when checked against the wrong expected address', () => {
  const key0 = deriveConsolidationKey(TEST_MNEMONIC, 0);
  const key1 = deriveConsolidationKey(TEST_MNEMONIC, 1);
  const { signedTransaction } = signTransaction(FIXTURE_TRANSACTION, key0.privateKey);
  // Signed with key0, but checked against key1's address — must be false,
  // this is exactly the CLAUDE.md安全鐵律4 self-check catching a wrong signer.
  assert.equal(verifySignerAddress(signedTransaction, key1.address), false);
});

test('verifySignerAddress rejects a malformed signature field', () => {
  const badTransaction = { ...FIXTURE_TRANSACTION, signature: ['not-a-real-signature'] };
  assert.throws(() => verifySignerAddress(badTransaction, 'TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH'), /signature/);
});
