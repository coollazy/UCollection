// storage.js runs on real browser IndexedDB in production; 驗證結論-07
// already proved the WebCrypto+IndexedDB combination works in a real
// Chrome tab across a genuine page reload. These tests exercise the exact
// same storage.js source (unmodified) under Node's native crypto.subtle
// against fake-indexeddb (a spec-compliant IndexedDB implementation) — this
// catches logic bugs in storage.js itself (key handling, error
// propagation, meta round-tripping) that a browser-only smoke test would
// not isolate as clearly. This is a convenience feature (CLAUDE.md: no
// fund-safety impact if broken — worst case the operator retypes the
// mnemonic), so it gets test coverage but not the same manual/Shasta
// scrutiny as sign.js/selfcheck.js.
import 'fake-indexeddb/auto';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { storeMnemonic, loadMnemonic, getMeta, deleteStored } from './storage.js';

const TEST_MNEMONIC = 'abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';

test('storeMnemonic + loadMnemonic round-trips the mnemonic exactly', async () => {
  const key = 'consolidation:1';
  await storeMnemonic(key, TEST_MNEMONIC, 'Test-Password-2026!', { label: 'wallet #1' });
  const got = await loadMnemonic(key, 'Test-Password-2026!');
  assert.equal(got, TEST_MNEMONIC);
});

test('loadMnemonic with the wrong password throws OperationError, never returns garbage', async () => {
  const key = 'consolidation:2';
  await storeMnemonic(key, TEST_MNEMONIC, 'Correct-Password!', {});
  await assert.rejects(() => loadMnemonic(key, 'Wrong-Password!'), (err) => {
    assert.equal(err.name, 'OperationError');
    return true;
  });
});

test('loadMnemonic returns null when nothing is stored under that key', async () => {
  const got = await loadMnemonic('consolidation:no-such-key', 'whatever');
  assert.equal(got, null);
});

test('getMeta returns the label without needing the password', async () => {
  const key = 'consolidation:3';
  await storeMnemonic(key, TEST_MNEMONIC, 'some-password', { label: '代收主錢包 #3，xpub末4碼...abcd' });
  const meta = await getMeta(key);
  assert.equal(meta.meta.label, '代收主錢包 #3，xpub末4碼...abcd');
  assert.equal(typeof meta.createdAt, 'number');
});

test('getMeta returns null when nothing is stored under that key', async () => {
  const meta = await getMeta('consolidation:no-such-key-either');
  assert.equal(meta, null);
});

test('different keys do not collide — consolidation and fee-topup entries stay independent', async () => {
  await storeMnemonic('consolidation:4', TEST_MNEMONIC, 'pw-a', { label: 'a' });
  await storeMnemonic('fee-topup:TSomeAddress', TEST_MNEMONIC, 'pw-b', { label: 'b' });

  const a = await loadMnemonic('consolidation:4', 'pw-a');
  const b = await loadMnemonic('fee-topup:TSomeAddress', 'pw-b');
  assert.equal(a, TEST_MNEMONIC);
  assert.equal(b, TEST_MNEMONIC);

  // wrong-key/password pairing must not decrypt either
  await assert.rejects(() => loadMnemonic('consolidation:4', 'pw-b'));
});

test('storeMnemonic overwrites an existing record under the same key', async () => {
  const key = 'consolidation:5';
  await storeMnemonic(key, TEST_MNEMONIC, 'first-password', { label: 'v1' });
  await storeMnemonic(key, TEST_MNEMONIC, 'second-password', { label: 'v2' });

  await assert.rejects(() => loadMnemonic(key, 'first-password'));
  const got = await loadMnemonic(key, 'second-password');
  assert.equal(got, TEST_MNEMONIC);
  const meta = await getMeta(key);
  assert.equal(meta.meta.label, 'v2');
});

test('deleteStored removes the record', async () => {
  const key = 'consolidation:6';
  await storeMnemonic(key, TEST_MNEMONIC, 'pw', {});
  await deleteStored(key);
  assert.equal(await getMeta(key), null);
  assert.equal(await loadMnemonic(key, 'pw'), null);
});
