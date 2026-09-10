// Orchestrates /admin/consolidation/sign: renders the batch's items from
// #page-data, wires the derive/sign buttons, and drives each item through
// prepare -> sign -> self-check -> broadcast (技術架構設計第10節「組交易與廣播」).
//
// The one rule this file must never violate: signTransaction()'s
// txIDMatched and verifySignerAddress()'s boolean are the ONLY two gates
// that decide whether /tron-proxy/.../broadcast gets called for an item
// (CLAUDE.md安全鐵律4). Every code path that reaches the broadcast fetch()
// call below has passed both. Nothing about UI state (button enabled,
// derive having "succeeded" earlier) substitutes for re-checking these on
// the transaction that is actually about to be broadcast.
import { deriveConsolidationKey, deriveFeeTopupKeyFromMnemonic, feeTopupKeyFromPrivateKeyHex } from './derive.js';
import { signTransaction } from './sign.js';
import { verifySignerAddress } from './selfcheck.js';
import { storeMnemonic, loadMnemonic, getMeta } from './storage.js';
import { sunToTrx, trxToSun } from './trxamount.js';

function readPageData() {
  const el = document.getElementById('page-data');
  return JSON.parse(el.textContent);
}

// storageKeyFor: consolidation mode's mnemonic is really the master
// wallet's own, so it's keyed by master_wallet_id; fee-topup mode's
// mnemonic/key belongs to an unrelated, operator-chosen address, so it's
// keyed by that address instead (see storage.js's file header for why
// these must not share a key).
function storageKeyFor(page) {
  return page.type === 'consolidation' ? `consolidation:${page.master_wallet_id}` : `fee-topup:${page.fee_source_address}`;
}

// postJSON's totpCode is only needed on the first call of a batch-processing
// run — auth.RequireTOTPCodeOrRecentStepUp (見ADR-0016「批次寬限」修訂) lets
// this session's later /tron-proxy/... calls through without one for a
// short window afterward, so a whole batch only prompts the operator once
// instead of twice per item.
async function postJSON(url, body, totpCode) {
  const headers = { 'Content-Type': 'application/json' };
  if (totpCode) headers['X-Totp-Code'] = totpCode;
  const resp = await fetch(url, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  });
  // A redirected response means RequireTOTPCode(OrRecentStepUp) intercepted
  // the request (missing/invalid/replayed code, or the batch grace window
  // expired) rather than the handler running — see auth.RequireTOTPCode's
  // doc comment. Its body is the returnTo page's HTML, not JSON.
  if (resp.redirected) {
    throw new Error('TOTP驗證碼有誤或已過期，請重新整理頁面、重新輸入驗證碼後再試一次');
  }
  const data = await resp.json().catch(() => null);
  if (!resp.ok) {
    const message = data && typeof data.message === 'string' ? data.message : `HTTP ${resp.status}`;
    throw new Error(message);
  }
  return data;
}

function statusLabel(status, errorDetail) {
  if (status === 'success') return '成功';
  if (status === 'broadcasting') return '已送出廣播，網路層無法立即確認結果（稍後回待歸集列表會自動核對鏈上狀態）';
  return `失敗：${errorDetail || '（無詳細訊息）'}`;
}

export function initSignPage() {
  const page = readPageData();

  const els = {
    itemsLoading: document.getElementById('items-loading'),
    itemsList: document.getElementById('items-list'),
    modeToggle: document.getElementById('input-mode-toggle'),
    mnemonicField: document.getElementById('mnemonic-field'),
    privkeyField: document.getElementById('privkey-field'),
    mnemonicInput: document.getElementById('mnemonic'),
    privkeyInput: document.getElementById('privkey-hex'),
    storageControls: document.getElementById('storage-controls'),
    deriveButton: document.getElementById('derive-button'),
    signButton: document.getElementById('sign-button'),
    signStatus: document.getElementById('sign-status'),
    totpCodeInput: document.getElementById('totp-code-input'),
  };

  // Set once per handleSignAndBroadcast() run, consumed by exactly the
  // first postJSON() call that run makes — every call after that omits the
  // header and relies on auth.RequireTOTPCodeOrRecentStepUp's batch grace
  // window (見postJSON's doc comment). Never populated ahead of the
  // operator clicking "簽名並廣播" for this run specifically.
  let pendingTOTPCode = null;
  function consumeTOTPCode() {
    const code = pendingTOTPCode;
    pendingTOTPCode = null;
    return code;
  }

  // Populated by handleDerive(), consumed by handleSignAndBroadcast().
  // Private key bytes live ONLY in this closure — never written back into
  // the DOM, never logged, never sent anywhere except as input to
  // sign.js's signTransaction().
  const derivedKeys = new Map(); // order_id -> Uint8Array
  let amountInput = null; // fee-topup only, created by renderItems()

  function currentInputMode() {
    if (page.type === 'consolidation') return 'mnemonic';
    const checked = document.querySelector('input[name="input-mode"]:checked');
    return checked ? checked.value : 'mnemonic';
  }

  function itemStatusEl(orderId) {
    return document.getElementById('item-status-' + orderId);
  }

  function renderItems() {
    els.itemsLoading.hidden = true;
    els.itemsList.hidden = false;
    els.itemsList.textContent = '';

    const summary = document.createElement('p');
    summary.textContent =
      page.type === 'consolidation'
        ? '目的地地址：' + page.destination_address
        : 'TRX來源地址：' + page.fee_source_address + '（分類：' + page.fee_source + '）';
    els.itemsList.appendChild(summary);

    const table = document.createElement('table');
    table.setAttribute('border', '1');
    table.setAttribute('cellpadding', '4');
    const headerRow = document.createElement('tr');
    ['訂單ID', '地址', '狀態'].forEach((text) => {
      const th = document.createElement('th');
      th.textContent = text;
      headerRow.appendChild(th);
    });
    table.appendChild(headerRow);

    for (const item of page.items) {
      const row = document.createElement('tr');
      const idCell = document.createElement('td');
      idCell.textContent = String(item.order_id);
      const addrCell = document.createElement('td');
      addrCell.textContent = item.address;
      const statusCell = document.createElement('td');
      statusCell.id = 'item-status-' + item.order_id;
      statusCell.textContent = '待核對';
      row.append(idCell, addrCell, statusCell);
      table.appendChild(row);
    }
    els.itemsList.appendChild(table);

    if (page.type === 'fee-topup') {
      const amountP = document.createElement('p');
      const label = document.createElement('label');
      label.append('每筆金額（TRX） ');
      amountInput = document.createElement('input');
      amountInput.type = 'text';
      if (page.default_amount_per_order) amountInput.value = sunToTrx(page.default_amount_per_order);
      label.appendChild(amountInput);
      amountP.appendChild(label);
      els.itemsList.appendChild(amountP);
    }
  }

  function wireModeToggle() {
    if (page.type !== 'fee-topup') return;
    els.modeToggle.hidden = false;
    document.querySelectorAll('input[name="input-mode"]').forEach((radio) => {
      radio.addEventListener('change', () => {
        const mode = currentInputMode();
        els.mnemonicField.hidden = mode !== 'mnemonic';
        els.privkeyField.hidden = mode !== 'privkey';
      });
    });
  }

  // renderStorageControls builds the "remember on this device" checkbox
  // and, if something is already stored under this page's key, a "load
  // stored value" control — entirely dynamic DOM (no innerHTML with
  // interpolated data anywhere in this file, even though CSP-wise it
  // wouldn't matter for a same-origin string; textContent/createElement
  // throughout is just good hygiene on a page that handles secrets).
  async function renderStorageControls() {
    const key = storageKeyFor(page);
    els.storageControls.textContent = '';

    const meta = await getMeta(key).catch(() => null);
    if (meta) {
      const info = document.createElement('p');
      info.textContent = '此裝置已儲存過這個錢包的助記詞/私鑰（' + new Date(meta.createdAt).toLocaleString() + '）。';
      const unlockInput = document.createElement('input');
      unlockInput.type = 'password';
      unlockInput.autocomplete = 'off';
      const unlockLabel = document.createElement('label');
      unlockLabel.append('本機加密密碼 ', unlockInput);
      const unlockButton = document.createElement('button');
      unlockButton.type = 'button';
      unlockButton.textContent = '載入已儲存的內容';
      unlockButton.addEventListener('click', () => {
        loadMnemonic(key, unlockInput.value)
          .then((value) => {
            if (value === null) return;
            fillInputWithStoredValue(value);
          })
          .catch(() => {
            els.signStatus.textContent = '本機加密密碼錯誤，無法載入';
          });
      });
      info.append(document.createElement('br'), unlockLabel, unlockButton);
      els.storageControls.appendChild(info);
    }

    const rememberCheckbox = document.createElement('input');
    rememberCheckbox.type = 'checkbox';
    const rememberLabel = document.createElement('label');
    rememberLabel.append(
      rememberCheckbox,
      ' 核對通過後，加密記住這台裝置上的助記詞/私鑰（僅供下次省得重新輸入，遺失本機資料不影響助記詞本身安全性，請務必另外備份助記詞原文——需求書5.9）',
    );

    const storagePasswordInput = document.createElement('input');
    storagePasswordInput.type = 'password';
    storagePasswordInput.autocomplete = 'off';
    storagePasswordInput.hidden = true;
    const storagePasswordLabel = document.createElement('label');
    storagePasswordLabel.append('本機加密密碼（與後台登入密碼無關，請另外記住） ', storagePasswordInput);
    rememberCheckbox.addEventListener('change', () => {
      storagePasswordInput.hidden = !rememberCheckbox.checked;
    });

    els.storageControls.append(document.createElement('p'));
    const p = els.storageControls.lastChild;
    p.append(rememberLabel, document.createElement('br'), storagePasswordLabel);

    // exposed for handleDerive() via closure below
    els._rememberCheckbox = rememberCheckbox;
    els._storagePasswordInput = storagePasswordInput;
  }

  // fillInputWithStoredValue guesses mnemonic vs private-key-hex purely by
  // shape (mnemonics are space-separated words, private keys are one
  // 64-char hex token) — storage.js itself doesn't track which kind was
  // stored, since to it a mnemonic and a private key hex are both just an
  // opaque string.
  function fillInputWithStoredValue(value) {
    const looksLikePrivateKey = /^[0-9a-fA-F]{64}$/.test(value.trim());
    if (looksLikePrivateKey && page.type === 'fee-topup') {
      els.privkeyInput.value = value;
      const privkeyRadio = document.querySelector('input[name="input-mode"][value="privkey"]');
      if (privkeyRadio) {
        privkeyRadio.checked = true;
        els.mnemonicField.hidden = true;
        els.privkeyField.hidden = false;
      }
    } else {
      els.mnemonicInput.value = value;
      const mnemonicRadio = document.querySelector('input[name="input-mode"][value="mnemonic"]');
      if (mnemonicRadio) {
        mnemonicRadio.checked = true;
        els.mnemonicField.hidden = false;
        els.privkeyField.hidden = true;
      }
    }
  }

  async function maybeRememberInput(mode, rawValue) {
    if (!els._rememberCheckbox || !els._rememberCheckbox.checked) return;
    const password = els._storagePasswordInput.value;
    if (!password) return;
    const key = storageKeyFor(page);
    const label =
      page.type === 'consolidation'
        ? '代收主錢包 #' + page.master_wallet_id + '，xpub末4碼...' + page.xpub.slice(-4)
        : 'TRX來源 ' + page.fee_source_address;
    await storeMnemonic(key, rawValue, password, { label });
  }

  // handleDerive validates the operator's input BEFORE any private key is
  // trusted for signing: consolidation mode requires the derived
  // account-level xpub to equal page.xpub (技術架構設計第10節「與該代收主錢包已存
  // 的master_wallets.xpub...逐字比對，不符則拒絕」) and every item's derived
  // address to equal its recorded address; fee-topup mode requires the
  // single derived address to equal page.fee_source_address. Any mismatch
  // aborts the whole batch — derivedKeys is only populated once every
  // check has passed.
  async function handleDerive() {
    els.signButton.disabled = true;
    els.signStatus.textContent = '核對中...';
    derivedKeys.clear();

    try {
      const mode = currentInputMode();
      let rawValue;

      if (page.type === 'consolidation') {
        rawValue = els.mnemonicInput.value;
        for (const item of page.items) {
          const derived = deriveConsolidationKey(rawValue, item.derivation_index);
          if (derived.xpub !== page.xpub) {
            throw new Error('衍生出的xpub與此代收主錢包不符，請確認助記詞是否正確');
          }
          if (derived.address !== item.address) {
            throw new Error('訂單 ' + item.order_id + ' 衍生地址與預期不符，已中止');
          }
          derivedKeys.set(item.order_id, derived.privateKey);
          itemStatusEl(item.order_id).textContent = '核對通過';
        }
      } else {
        rawValue = mode === 'mnemonic' ? els.mnemonicInput.value : els.privkeyInput.value;
        const derived = mode === 'mnemonic' ? deriveFeeTopupKeyFromMnemonic(rawValue) : feeTopupKeyFromPrivateKeyHex(rawValue);
        if (derived.address !== page.fee_source_address) {
          throw new Error('衍生地址與TRX來源地址不符，請確認輸入是否正確');
        }
        for (const item of page.items) {
          derivedKeys.set(item.order_id, derived.privateKey);
          itemStatusEl(item.order_id).textContent = '核對通過';
        }
      }

      // Storing locally is a convenience, not a requirement — a failure
      // here (e.g. IndexedDB unavailable in a private browsing context)
      // must not block the operator from signing/broadcasting, but the
      // status text must reflect it rather than being silently overwritten
      // by the generic success message right below.
      const rememberFailed = await maybeRememberInput(mode, rawValue).then(
        () => false,
        () => true,
      );

      els.signStatus.textContent = rememberFailed
        ? '核對通過，可以簽名並廣播（本機加密儲存失敗，已略過，不影響本次簽名）'
        : '核對通過，可以簽名並廣播';
      els.signButton.disabled = false;
    } catch (err) {
      derivedKeys.clear();
      els.signStatus.textContent = '核對失敗：' + err.message;
    }
  }

  // processOneItem is the only place in this file that calls
  // /tron-proxy/.../broadcast. It reaches that call ONLY after
  // signTransaction() reports txIDMatched and verifySignerAddress()
  // returns true — see this file's header comment.
  async function processOneItem(item) {
    const privateKey = derivedKeys.get(item.order_id);
    if (!privateKey) throw new Error('尚未核對，已略過');

    itemStatusEl(item.order_id).textContent = '準備交易中...';
    const prepareBody =
      page.type === 'consolidation'
        ? { batch_id: page.batch_id, order_id: item.order_id }
        : { batch_id: page.batch_id, order_id: item.order_id, amount: trxToSun(amountInput.value) };
    const prepareUrl = page.type === 'consolidation' ? '/tron-proxy/consolidation/prepare' : '/tron-proxy/fee-topup/prepare';
    const prepared = await postJSON(prepareUrl, prepareBody, consumeTOTPCode());

    itemStatusEl(item.order_id).textContent = '簽名中...';
    const { signedTransaction, txIDMatched } = signTransaction(prepared.transaction, privateKey);
    if (!txIDMatched) {
      throw new Error('本地重算txID與伺服器回傳不符，已中止，未廣播');
    }

    const expectedAddress = page.type === 'consolidation' ? item.address : page.fee_source_address;
    if (!verifySignerAddress(signedTransaction, expectedAddress)) {
      throw new Error('簽名反推地址與預期不符，已中止，未廣播');
    }

    itemStatusEl(item.order_id).textContent = '廣播中...';
    const broadcastUrl = page.type === 'consolidation' ? '/tron-proxy/consolidation/broadcast' : '/tron-proxy/fee-topup/broadcast';
    const result = await postJSON(broadcastUrl, { item_id: prepared.item_id, transaction: signedTransaction }, consumeTOTPCode());
    itemStatusEl(item.order_id).textContent = statusLabel(result.status, result.error_detail);
  }

  // handleSignAndBroadcast processes items one at a time (not in
  // parallel) purely for legible sequential UI feedback — Tron has no
  // account nonce, so this is not a correctness requirement (驗證結論-06
  // 附加測試 already confirmed concurrent broadcasts don't interfere). One
  // item failing must not stop the rest (技術架構設計第10節「逐筆獨立追蹤」).
  async function handleSignAndBroadcast() {
    const totpCode = els.totpCodeInput.value.trim();
    if (!totpCode) {
      els.signStatus.textContent = '請輸入TOTP驗證碼';
      return;
    }
    pendingTOTPCode = totpCode;
    els.signButton.disabled = true;
    els.deriveButton.disabled = true;
    for (const item of page.items) {
      await processOneItem(item).catch((err) => {
        itemStatusEl(item.order_id).textContent = '失敗：' + err.message;
      });
    }
    els.signStatus.textContent = '批次處理完成，請回待歸集列表確認每筆最終結果';
    els.deriveButton.disabled = false;
  }

  renderItems();

  if (page.items.length === 0) {
    // Nothing to sign — every order_id in the query string was either
    // absent or filtered out by signPageHandler (e.g. belonged to a
    // different master wallet). Refuse to let the operator type a
    // mnemonic into a page that has nothing to do with it.
    els.signStatus.textContent = '此批次沒有任何有效項目，請回待歸集列表重新操作';
    els.deriveButton.disabled = true;
    els.signButton.disabled = true;
    return;
  }

  wireModeToggle();
  renderStorageControls();
  els.deriveButton.addEventListener('click', () => {
    handleDerive();
  });
  els.signButton.addEventListener('click', () => {
    handleSignAndBroadcast();
  });
}
