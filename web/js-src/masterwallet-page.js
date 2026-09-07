// Orchestration for /admin/master-wallets/new (技術架構設計第11節「新增」). Two
// input paths — 方式1「輸入既有助記詞」、方式2「產生新錢包」— both end at the same
// xpub-only derivation (deriveAccountXpub) and the same fetch() POST to
// /admin/master-wallets. No private key ever leaves this module (CLAUDE.md
// 安全鐵律1) — only the derived xpub (public) is sent to the backend.
import { generateMnemonic, validateMnemonic } from 'bip39';
import { deriveAccountXpub } from './derive.js';

const BACKUP_VERIFY_WORD_COUNT = 4;

let pendingXpub = null;
let pendingGeneratedMnemonic = null;

export function initMasterWalletPage() {
  document.getElementById('mode-loading').hidden = true;
  document.getElementById('mode-select').hidden = false;
  document.getElementById('existing-mode').hidden = false;

  for (const radio of document.querySelectorAll('input[name="wallet-mode"]')) {
    radio.addEventListener('change', onModeChange);
  }
  document.getElementById('existing-derive-button').addEventListener('click', onExistingDerive);
  document.getElementById('generate-button').addEventListener('click', onGenerate);
  document.getElementById('verify-backup-button').addEventListener('click', onVerifyBackup);
  document.getElementById('submit-button').addEventListener('click', onSubmit);
}

function onModeChange(e) {
  const isExisting = e.target.value === 'existing';
  document.getElementById('existing-mode').hidden = !isExisting;
  document.getElementById('new-mode').hidden = isExisting;
  hideConfirmBox();
  setStatus('');
}

function onExistingDerive() {
  const mnemonic = document.getElementById('existing-mnemonic').value.trim();
  if (!validateMnemonic(mnemonic)) {
    setStatus('助記詞格式不正確，請重新輸入');
    return;
  }
  try {
    showConfirm(deriveAccountXpub(mnemonic));
  } catch (err) {
    setStatus(err.message || String(err));
  }
}

function onGenerate() {
  const mnemonic = generateMnemonic();
  pendingGeneratedMnemonic = mnemonic;
  document.getElementById('generated-mnemonic-text').textContent = mnemonic;
  document.getElementById('generated-mnemonic-box').hidden = false;

  const words = mnemonic.split(' ');
  const positions = randomDistinctPositions(words.length, BACKUP_VERIFY_WORD_COUNT);
  const container = document.getElementById('backup-verify-fields');
  container.innerHTML = '';
  for (const pos of positions) {
    const label = document.createElement('label');
    label.textContent = `第 ${pos + 1} 個字 `;
    const input = document.createElement('input');
    input.type = 'text';
    input.dataset.position = String(pos);
    input.autocomplete = 'off';
    label.appendChild(input);
    const p = document.createElement('p');
    p.appendChild(label);
    container.appendChild(p);
  }
  hideConfirmBox();
  setStatus('');
}

function onVerifyBackup() {
  if (!pendingGeneratedMnemonic) {
    setStatus('請先產生新助記詞');
    return;
  }
  const words = pendingGeneratedMnemonic.split(' ');
  const inputs = document.querySelectorAll('#backup-verify-fields input');
  for (const input of inputs) {
    const pos = Number(input.dataset.position);
    const want = words[pos];
    const got = input.value.trim().toLowerCase();
    if (got !== want) {
      setStatus('備份驗證失敗，請對照上方助記詞重新輸入');
      return;
    }
  }
  try {
    showConfirm(deriveAccountXpub(pendingGeneratedMnemonic));
    setStatus('備份驗證通過');
  } catch (err) {
    setStatus(err.message || String(err));
  }
}

function onSubmit() {
  if (!pendingXpub) {
    setStatus('尚未算出xpub，無法送出');
    return;
  }
  const totpCode = document.getElementById('totp-code-input').value.trim();
  if (!totpCode) {
    setStatus('請輸入TOTP驗證碼');
    return;
  }
  if (!window.confirm('確定要送出這組代收主錢包嗎？若目前已有使用中的代收主錢包，將自動轉為已停用，之後所有新訂單將改用這一組衍生收款地址。')) {
    return;
  }
  setStatus('送出中……');
  fetch('/admin/master-wallets', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Totp-Code': totpCode },
    body: JSON.stringify({ xpub: pendingXpub }),
  })
    .then((resp) => {
      // A redirected response means RequireTOTPCode intercepted the
      // request before it ever reached the handler (missing/invalid/
      // replayed code) — its body is the returnTo page's HTML, not JSON.
      // See auth.RequireTOTPCode's doc comment.
      if (resp.redirected) {
        setStatus('TOTP驗證碼有誤或已過期，請重新輸入後再送出一次');
        return null;
      }
      return resp.json().then((data) => ({ status: resp.status, data }));
    })
    .then((result) => {
      if (!result) return;
      const { status, data } = result;
      if (status === 200 && data.ok) {
        window.location.href = data.redirect || '/admin/master-wallets';
        return;
      }
      if (data.error === 'duplicate') {
        setStatus('此xpub已存在，請改用列表頁的「恢復使用中」，或前往列表頁');
        return;
      }
      setStatus('送出失敗：' + (data.error || status));
    })
    .catch((err) => {
      setStatus('送出失敗：' + (err.message || String(err)));
    });
}

function showConfirm(xpub) {
  pendingXpub = xpub;
  document.getElementById('confirm-xpub-tail').textContent = xpub.slice(-4);
  document.getElementById('confirm-box').hidden = false;
}

function hideConfirmBox() {
  pendingXpub = null;
  document.getElementById('confirm-box').hidden = true;
}

function setStatus(text) {
  document.getElementById('wallet-status').textContent = text;
}

function randomDistinctPositions(total, count) {
  const positions = new Set();
  while (positions.size < Math.min(count, total)) {
    positions.add(Math.floor(Math.random() * total));
  }
  return [...positions].sort((a, b) => a - b);
}
