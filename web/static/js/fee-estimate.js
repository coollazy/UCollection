// 待歸集頁的手續費試算 UI 增強（手動歸集流程重構 Phase 3，見 docs/進度.md）：
// 訂單列表全選、手續費支付方式大類切換（目前只有「支付TRX」，為未來能量方案
// 預留分類）、查TRX餘額、手續費試算並隨勾選數量即時重算加總。零依賴 vanilla
// JS，比照 address-book-select.js 的風格；本頁無嚴格CSP，同源fetch免CSRF
// token（同源請求本身已過 Origin 檢查）。
//
// 本檔案不動「開始歸集」/「補充手續費」兩個送出按鈕與表單送出邏輯——送出流程
// 改造是下一個 Phase 的事，這裡只加唯讀試算/查詢與純前端的顯示切換。
(function () {
  'use strict';

  // 手續費試算結果快取：{firstSun, repeatSun, firstTRX, repeatTRX, firstEnergy}。
  // 勾選數量變動時只用這份快取重算加總，不重打API；改目的地才需要重按試算。
  var lastEstimate = null;

  function $(sel) {
    return document.querySelector(sel);
  }
  function $all(sel) {
    return document.querySelectorAll(sel);
  }

  function orderCheckboxes() {
    return $all('input[name="order_id"]');
  }

  function selectedOrderIds() {
    var boxes = $all('input[name="order_id"]:checked');
    var ids = [];
    for (var i = 0; i < boxes.length; i++) ids.push(Number(boxes[i].value));
    return ids;
  }

  // sun（最小單位整數）轉TRX顯示字串，去尾零，純整數運算——邏輯比照後端
  // internal/consolidation/templates.go 的 formatMicroAmount，只是這裡只用
  // 在前端自算的「加總」數字上；單筆首/其餘筆金額一律直接顯示端點回傳的
  // first_trx/repeat_trx字串，不重算（CLAUDE.md 安全鐵律6：金額運算禁止浮點）。
  function formatSunAsTRX(sun) {
    var whole = Math.trunc(sun / 1000000);
    var frac = sun % 1000000;
    if (frac === 0) return String(whole);
    var fracStr = String(frac);
    while (fracStr.length < 6) fracStr = '0' + fracStr;
    fracStr = fracStr.replace(/0+$/, '');
    return whole + '.' + fracStr;
  }

  function renderEstimate() {
    var container = $('#fee-estimate-result');
    if (!container || !lastEstimate) return;
    var n = selectedOrderIds().length;
    if (n === 0) {
      container.textContent = '請先勾選至少一筆訂單';
      return;
    }
    var sumSun = lastEstimate.firstSun + lastEstimate.repeatSun * (n - 1);
    container.innerHTML =
      '<p>首筆：' + lastEstimate.firstTRX + ' TRX（' + lastEstimate.firstEnergy + ' 能量）</p>' +
      '<p>其餘每筆：' + lastEstimate.repeatTRX + ' TRX</p>' +
      '<p>加總（' + n + ' 筆）：' + formatSunAsTRX(sumSun) + ' TRX</p>';
  }

  // ---- 1. 全選 ----
  function initSelectAll() {
    var selectAll = $('#select-all-orders');
    if (!selectAll) return;
    selectAll.addEventListener('change', function () {
      var boxes = orderCheckboxes();
      for (var i = 0; i < boxes.length; i++) boxes[i].checked = selectAll.checked;
      renderEstimate();
    });
  }

  // ---- 勾選數量變動時即時重算加總 ----
  function initOrderCheckboxWatchers() {
    var boxes = orderCheckboxes();
    for (var i = 0; i < boxes.length; i++) {
      boxes[i].addEventListener('change', renderEstimate);
    }
  }

  // ---- 2. 手續費支付方式大類：選「支付TRX」才顯示來源地址等欄位 ----
  function initFeePaymentMethod() {
    var select = $('#fee-payment-method');
    var trxFields = $('#fee-trx-fields');
    if (!select || !trxFields) return;
    function sync() {
      trxFields.hidden = select.value !== 'trx';
    }
    select.addEventListener('change', sync);
    sync(); // 依初始選項設定顯示狀態（預設「支付TRX」即顯示）
  }

  // ---- 3. 查TRX餘額 ----
  function initTRXBalanceCheck() {
    var btn = $('#check-trx-balance-btn');
    var addrInput = $('#fee-source-address');
    var result = $('#trx-balance-result');
    if (!btn || !addrInput || !result) return;

    btn.addEventListener('click', function () {
      var address = addrInput.value.trim();
      if (!address) {
        result.textContent = '請先輸入來源地址';
        return;
      }
      result.textContent = '查詢中…';
      fetch('/admin/consolidation/trx-balance', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ address: address }),
      })
        .then(function (r) {
          if (!r.ok) throw new Error('trx-balance request failed');
          return r.json();
        })
        .then(function (data) {
          result.textContent = '目前餘額：' + data.balance_trx + ' TRX';
        })
        .catch(function () {
          result.textContent = '查詢失敗，請確認地址是否正確';
        });
    });
  }

  // ---- 4. 手續費試算 ----
  function initFeeEstimate() {
    var btn = $('#fee-estimate-btn');
    var container = $('#fee-estimate-result');
    var destInput = $('#dest-addr');
    var walletInput = $('input[name="master_wallet_id"]');
    if (!btn || !container) return;

    btn.addEventListener('click', function () {
      var orderIds = selectedOrderIds();
      if (orderIds.length === 0) {
        lastEstimate = null;
        container.textContent = '請先勾選至少一筆訂單';
        return;
      }
      container.textContent = '試算中…';
      fetch('/admin/consolidation/fee-estimate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          master_wallet_id: Number(walletInput ? walletInput.value : 0),
          destination_address: destInput ? destInput.value.trim() : '',
          order_ids: orderIds,
        }),
      })
        .then(function (r) {
          if (!r.ok) throw new Error('fee-estimate request failed');
          return r.json();
        })
        .then(function (data) {
          lastEstimate = {
            firstSun: data.first_trx_sun,
            repeatSun: data.repeat_trx_sun,
            firstTRX: data.first_trx,
            repeatTRX: data.repeat_trx,
            firstEnergy: data.first_energy,
          };
          renderEstimate();
        })
        .catch(function () {
          lastEstimate = null;
          container.textContent = '試算失敗，請確認目的地地址是否正確或稍後再試';
        });
    });
  }

  initSelectAll();
  initOrderCheckboxWatchers();
  initFeePaymentMethod();
  initTRXBalanceCheck();
  initFeeEstimate();
})();
