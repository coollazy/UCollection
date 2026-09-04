// Plain vanilla JS, no build step, no npm dependencies — this is NOT the
// offline-bundled mnemonic/signing JS (that's web/static/js/*.bundle.js,
// esbuild-packaged, see docs/開發流程框架-03-技術架構設計.md 第10節). This file
// only exists because HTML forms can't natively submit a DELETE request.

function deleteAddressBookEntry(id) {
  fetch(`/admin/consolidation/address-book/${id}`, { method: 'DELETE' })
    .then((r) => {
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
