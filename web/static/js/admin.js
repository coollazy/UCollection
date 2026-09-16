// Plain vanilla JS, no build step, no npm dependencies — this is NOT the
// offline-bundled mnemonic/signing JS (that's web/static/js/*.bundle.js,
// esbuild-packaged, see docs/開發流程框架-03-技術架構設計.md 第10節). This file
// only exists because HTML forms can't natively submit a DELETE request.

function deleteAddressBookEntry(id) {
  const totpCode = prompt('請輸入TOTP驗證碼以確認刪除：');
  if (!totpCode) return;
  fetch(`/admin/consolidation/address-book/${id}`, {
    method: 'DELETE',
    headers: { 'X-Totp-Code': totpCode },
  })
    .then((r) => {
      // A redirected response means RequireTOTPCode intercepted the request
      // (missing/invalid/replayed code) rather than the DELETE completing —
      // see auth.RequireTOTPCode's doc comment.
      if (r.redirected) {
        alert('TOTP驗證碼有誤或已過期，請重新操作一次');
        return;
      }
      if (r.ok) {
        location.reload();
      } else {
        alert('刪除失敗');
      }
    })
    .catch(() => {
      alert('刪除失敗');
    });
}

// copyToClipboard copies `text` to the clipboard and briefly flashes a
// confirmation on the triggering button. navigator.clipboard requires a
// secure context (HTTPS or localhost) — ADR-0004（不內建反向代理/TLS終止）意味著
// 商戶可能直接以純HTTP方式跑後台，所以在Clipboard API不可用時退回舊式
// execCommand('copy') hidden-textarea寫法。
function copyToClipboard(text, btn) {
  function flash(ok) {
    if (!btn) return;
    const original = btn.textContent;
    btn.textContent = ok ? '已複製' : '複製失敗';
    setTimeout(() => {
      btn.textContent = original;
    }, 1500);
  }
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).then(
      () => flash(true),
      () => flash(false)
    );
    return;
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  try {
    document.execCommand('copy');
    flash(true);
  } catch (e) {
    flash(false);
  }
  document.body.removeChild(ta);
}

// deleteFeeSourceBookEntry is deleteAddressBookEntry's counterpart for the
// 手續費來源地址簿 (需求書5.9 v0.39) — same DELETE-via-fetch mechanism, just a
// different endpoint. The two address books are two independent lists.
function deleteFeeSourceBookEntry(id) {
  const totpCode = prompt('請輸入TOTP驗證碼以確認刪除：');
  if (!totpCode) return;
  fetch(`/admin/consolidation/fee-source-book/${id}`, {
    method: 'DELETE',
    headers: { 'X-Totp-Code': totpCode },
  })
    .then((r) => {
      if (r.redirected) {
        alert('TOTP驗證碼有誤或已過期，請重新操作一次');
        return;
      }
      if (r.ok) {
        location.reload();
      } else {
        alert('刪除失敗');
      }
    })
    .catch(() => {
      alert('刪除失敗');
    });
}
