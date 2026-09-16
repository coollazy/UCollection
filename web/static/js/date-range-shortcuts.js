// 訂單列表「建立時間」快速區間按鈕。只負責把日期填入 #created-from/#created-to
// 兩個既有的 <input type="date">，不自動送出查詢——跟其他篩選欄位一致，統一由
// 使用者按「查詢」才送出，方便同時調整狀態/其他欄位再一次查詢。
(function () {
  function pad(n) {
    return String(n).padStart(2, '0');
  }

  function toDateInputValue(d) {
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
  }

  function rangeFor(key) {
    var now = new Date();
    var today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    switch (key) {
      case 'today':
        return [today, today];
      case 'yesterday': {
        var y = new Date(today);
        y.setDate(y.getDate() - 1);
        return [y, y];
      }
      case 'last7': {
        var from7 = new Date(today);
        from7.setDate(from7.getDate() - 6);
        return [from7, today];
      }
      case 'last30': {
        var from30 = new Date(today);
        from30.setDate(from30.getDate() - 29);
        return [from30, today];
      }
      case 'this-month':
        return [new Date(today.getFullYear(), today.getMonth(), 1), today];
      case 'last-month': {
        var lastMonthStart = new Date(today.getFullYear(), today.getMonth() - 1, 1);
        var lastMonthEnd = new Date(today.getFullYear(), today.getMonth(), 0);
        return [lastMonthStart, lastMonthEnd];
      }
      default:
        return null;
    }
  }

  var fromInput = document.getElementById('created-from');
  var toInput = document.getElementById('created-to');
  if (!fromInput || !toInput) return;

  document.querySelectorAll('[data-date-range]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var range = rangeFor(btn.getAttribute('data-date-range'));
      if (!range) return;
      fromInput.value = toDateInputValue(range[0]);
      toInput.value = toDateInputValue(range[1]);
    });
  });
})();
