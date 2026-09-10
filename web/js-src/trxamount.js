// TRX 顯示/輸入用小數，但廣播與後端全程用 int64 最小單位 sun（6 位小數，
// 安全鐵律6）。這兩個工具在 UI 邊界做 sun<->TRX 轉換，純字串處理、零浮點
// 乘除（比照後端 parseTRXAmount / parseUSDTAmount）。TRX 的 sun 與 USDT-TRC20
// 皆 6 位小數，邏輯相同。

// sunToTrx: int64 sun -> 去尾零的 TRX 小數字串（3000000 -> "3", 3500000 ->
// "3.5", 1 -> "0.000001", 17000000 -> "17"）。
export function sunToTrx(sun) {
  const padded = String(sun).padStart(7, '0'); // 至少 1 位整數 + 6 位小數
  const intPart = padded.slice(0, -6).replace(/^0+(?=\d)/, '');
  const fracPart = padded.slice(-6).replace(/0+$/, '');
  return fracPart ? `${intPart}.${fracPart}` : intPart;
}

// trxToSun: TRX 小數字串 -> int64 sun。格式錯（空/負/超過6位小數/非數字/<=0）
// 一律 throw，讓呼叫端在廣播前中止而非送出錯誤金額。
export function trxToSun(s) {
  s = String(s).trim();
  if (s === '' || s.startsWith('-')) throw new Error('每筆金額須為大於0的數字');
  const [intPart, fracPart = ''] = s.split('.');
  if (fracPart.length > 6) throw new Error('每筆金額最多6位小數');
  const combined = (intPart || '0') + fracPart.padEnd(6, '0');
  if (!/^\d+$/.test(combined)) throw new Error('無法辨識的每筆金額格式');
  const sun = Number(combined);
  if (!Number.isSafeInteger(sun) || sun <= 0) throw new Error('每筆金額須為大於0的數字');
  return sun;
}
