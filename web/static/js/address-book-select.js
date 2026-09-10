// 待歸集頁的地址簿快速選取（第9/10項）。每個帶 data-target-input 的 <select>
// 綁定一個目標 <input id="...">：選地址簿項目時把地址寫入 input 並隱藏 input；
// 選「手動輸入新地址」（value="__manual__"）時顯示 input 供自由輸入。
//
// input 一律是實際送出的欄位（帶 name），select 無 name、純 UI 控制、不送出——
// 後端維持只讀 destination_address/fee_source_address 一個值，無須改動。JS 未
// 執行時 select 保持 hidden、input 可見，退化成原本的純文字輸入框。
(function () {
  var MANUAL = '__manual__';

  function bind(select) {
    var input = document.getElementById(select.getAttribute('data-target-input'));
    if (!input) return;

    function sync(userInitiated) {
      if (select.value === MANUAL) {
        input.hidden = false;
        input.value = '';
        if (userInitiated) input.focus();
      } else {
        input.value = select.value;
        input.hidden = true;
      }
    }

    select.hidden = false; // JS 接管後才顯示 select（未執行時維持純文字框）
    select.addEventListener('change', function () { sync(true); });
    sync(false); // 依初始選項設定 input 狀態，勿在載入時搶焦點
  }

  var selects = document.querySelectorAll('select[data-target-input]');
  for (var i = 0; i < selects.length; i++) bind(selects[i]);
})();
