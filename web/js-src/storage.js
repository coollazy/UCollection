// Optional local convenience: encrypt the operator's mnemonic and keep it
// in this browser's IndexedDB so it doesn't need retyping every visit
// (技術架構設計第10節「本機加密儲存」, 驗證結論-07 already proved this exact
// WebCrypto/IndexedDB combination). This is convenience only, not a backup
// — losing browser storage does not lose the mnemonic itself, the operator
// must still have it recorded elsewhere (需求書5.9). Nothing here is
// required for signing/broadcasting to work; the sign page always accepts
// direct mnemonic/private-key entry regardless of what's stored.
//
// Storage key is caller-chosen, not hardcoded to "master wallet id" —
// consolidation mode's mnemonic really is the master wallet's own
// (key = "consolidation:{master_wallet_id}"), but fee-topup mode's
// mnemonic belongs to an arbitrary, unrelated address
// (key = "fee-topup:{fee_source_address}") — using the master_wallet_id
// there would conflate two different wallets under one storage slot.

const DB_NAME = 'ucollection_consolidation_sign';
const DB_VERSION = 1;
const STORE_NAME = 'encrypted_mnemonics';

// 技術架構設計第10節「本機加密儲存」正式拍板值（驗證結論-07測試用300,000僅為證明流程可
// 行，非最終參數）：OWASP Password Storage Cheat Sheet對PBKDF2-HMAC-SHA256建議下限。
const PBKDF2_ITERATIONS = 600000;
const SALT_LENGTH_BYTES = 16;
const IV_LENGTH_BYTES = 12;

function openDB() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      req.result.createObjectStore(STORE_NAME, { keyPath: 'key' });
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

// requestToPromise wraps a single IDBRequest, not a whole transaction —
// this is deliberately simpler than tracking tx.oncomplete, since every
// operation in this file issues exactly one request per transaction.
function requestToPromise(req) {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function deriveAesKey(password, salt, iterations) {
  const passwordKey = await crypto.subtle.importKey('raw', new TextEncoder().encode(password), 'PBKDF2', false, [
    'deriveKey',
  ]);
  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt, iterations, hash: 'SHA-256' },
    passwordKey,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  );
}

// storeMnemonic encrypts mnemonic with a key derived from password and
// writes it to IndexedDB under key, overwriting any existing record for
// the same key. meta is stored UNENCRYPTED alongside the ciphertext — it
// must never contain the mnemonic or any secret, only a display label
// (e.g. "代收主錢包 #3，xpub末4碼...abcd，2026-09-05建立") so getMeta() can
// show "this device already has something stored for this wallet" without
// requiring the password first.
export async function storeMnemonic(key, mnemonic, password, meta) {
  const salt = crypto.getRandomValues(new Uint8Array(SALT_LENGTH_BYTES));
  const iv = crypto.getRandomValues(new Uint8Array(IV_LENGTH_BYTES));
  const aesKey = await deriveAesKey(password, salt, PBKDF2_ITERATIONS);
  const ciphertext = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, aesKey, new TextEncoder().encode(mnemonic));

  const db = await openDB();
  try {
    const store = db.transaction(STORE_NAME, 'readwrite').objectStore(STORE_NAME);
    await requestToPromise(store.put({ key, salt, iv, ciphertext, iterations: PBKDF2_ITERATIONS, meta, createdAt: Date.now() }));
  } finally {
    db.close();
  }
}

async function getRecord(key) {
  const db = await openDB();
  try {
    const store = db.transaction(STORE_NAME, 'readonly').objectStore(STORE_NAME);
    return await requestToPromise(store.get(key));
  } finally {
    db.close();
  }
}

// loadMnemonic decrypts and returns the mnemonic string for key, or null
// if nothing is stored under that key. A wrong password makes
// crypto.subtle.decrypt() throw DOMException('OperationError') — AES-GCM's
// authentication tag check fails closed, it never silently returns garbage
// (驗證結論-07 already confirmed this). Callers should let that exception
// propagate to the UI as "密碼錯誤", not swallow it.
export async function loadMnemonic(key, password) {
  const record = await getRecord(key);
  if (!record) return null;
  const aesKey = await deriveAesKey(password, record.salt, record.iterations);
  const plaintext = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: record.iv }, aesKey, record.ciphertext);
  return new TextDecoder().decode(plaintext);
}

// getMeta returns { meta, createdAt } without needing the password, so the
// page can show "此裝置已儲存過這個錢包的助記詞" before the operator types
// anything. Returns null if nothing is stored under key.
export async function getMeta(key) {
  const record = await getRecord(key);
  if (!record) return null;
  return { meta: record.meta, createdAt: record.createdAt };
}

export async function deleteStored(key) {
  const db = await openDB();
  try {
    const store = db.transaction(STORE_NAME, 'readwrite').objectStore(STORE_NAME);
    await requestToPromise(store.delete(key));
  } finally {
    db.close();
  }
}
