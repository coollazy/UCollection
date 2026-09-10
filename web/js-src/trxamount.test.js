import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sunToTrx, trxToSun } from './trxamount.js';

test('trxToSun: 整數與小數（字串法，零浮點）', () => {
  assert.equal(trxToSun('3'), 3000000);
  assert.equal(trxToSun('3.5'), 3500000);
  assert.equal(trxToSun('0.000001'), 1);
  assert.equal(trxToSun('17'), 17000000);
  assert.equal(trxToSun(' 3.5 '), 3500000); // 前後空白
  assert.equal(trxToSun('.5'), 500000); // 無整數部分
  assert.equal(trxToSun('0.1'), 100000); // 浮點乘法會有誤差的案例
});

test('trxToSun: 非法輸入一律 throw', () => {
  assert.throws(() => trxToSun(''));
  assert.throws(() => trxToSun('-1'));
  assert.throws(() => trxToSun('0'));
  assert.throws(() => trxToSun('3.1234567')); // 超過6位小數
  assert.throws(() => trxToSun('abc'));
  assert.throws(() => trxToSun('3.5x'));
});

test('sunToTrx: 去尾零顯示', () => {
  assert.equal(sunToTrx(3000000), '3');
  assert.equal(sunToTrx(3500000), '3.5');
  assert.equal(sunToTrx(1), '0.000001');
  assert.equal(sunToTrx(17000000), '17');
  assert.equal(sunToTrx(100000), '0.1');
  assert.equal(sunToTrx(123456), '0.123456');
});

test('往返一致：trxToSun(sunToTrx(x)) === x', () => {
  for (const sun of [1, 100000, 3000000, 3500000, 17000000, 123456, 44000000]) {
    assert.equal(trxToSun(sunToTrx(sun)), sun);
  }
});
