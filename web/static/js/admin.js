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
