/**
 * Lexicographic fractional order keys (R-ORG-6). A port of the "fractional-indexing"
 * algorithm, identical to server/internal/position: server items and device overlay
 * items share one key space and merge by key, ties broken by ID.
 */
const DIGITS = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
const SMALLEST_INTEGER = 'A' + '0'.repeat(26);

function midpoint(a: string, b: string | null): string {
  const zero = DIGITS[0]!;
  if (b !== null && a >= b) throw new Error(`${a} >= ${b}`);
  if (a.slice(-1) === zero || (b && b.slice(-1) === zero)) throw new Error('trailing zero');
  if (b) {
    let n = 0;
    while ((a[n] ?? zero) === b[n]) n++;
    if (n > 0) return b.slice(0, n) + midpoint(a.slice(n), b.slice(n));
  }
  const digitA = a ? DIGITS.indexOf(a[0]!) : 0;
  const digitB = b !== null ? DIGITS.indexOf(b[0]!) : DIGITS.length;
  if (digitB - digitA > 1) {
    return DIGITS[Math.round(0.5 * (digitA + digitB))]!;
  }
  if (b && b.length > 1) return b.slice(0, 1);
  return DIGITS[digitA]! + midpoint(a.slice(1), null);
}

function integerLength(head: string): number {
  if (head >= 'a' && head <= 'z') return head.charCodeAt(0) - 'a'.charCodeAt(0) + 2;
  if (head >= 'A' && head <= 'Z') return 'Z'.charCodeAt(0) - head.charCodeAt(0) + 2;
  throw new Error('invalid order key head: ' + head);
}

function integerPart(key: string): string {
  const n = integerLength(key[0]!);
  if (n > key.length) throw new Error('invalid order key: ' + key);
  return key.slice(0, n);
}

function validate(key: string): void {
  if (key === SMALLEST_INTEGER) throw new Error('invalid order key: ' + key);
  const i = integerPart(key);
  const f = key.slice(i.length);
  if (f.slice(-1) === DIGITS[0]) throw new Error('invalid order key: ' + key);
}

function increment(x: string): string | null {
  const [head, ...digs] = x.split('') as [string, ...string[]];
  let carry = true;
  for (let i = digs.length - 1; carry && i >= 0; i--) {
    const d = DIGITS.indexOf(digs[i]!) + 1;
    if (d === DIGITS.length) digs[i] = DIGITS[0]!;
    else {
      digs[i] = DIGITS[d]!;
      carry = false;
    }
  }
  if (carry) {
    if (head === 'Z') return 'a' + DIGITS[0];
    if (head === 'z') return null;
    const h = String.fromCharCode(head.charCodeAt(0) + 1);
    if (h > 'a') digs.push(DIGITS[0]!);
    else digs.pop();
    return h + digs.join('');
  }
  return head + digs.join('');
}

function decrement(x: string): string | null {
  const [head, ...digs] = x.split('') as [string, ...string[]];
  let borrow = true;
  for (let i = digs.length - 1; borrow && i >= 0; i--) {
    const d = DIGITS.indexOf(digs[i]!) - 1;
    if (d === -1) digs[i] = DIGITS.slice(-1);
    else {
      digs[i] = DIGITS[d]!;
      borrow = false;
    }
  }
  if (borrow) {
    if (head === 'a') return 'Z' + DIGITS.slice(-1);
    if (head === 'A') return null;
    const h = String.fromCharCode(head.charCodeAt(0) - 1);
    if (h < 'Z') digs.push(DIGITS.slice(-1));
    else digs.pop();
    return h + digs.join('');
  }
  return head + digs.join('');
}

/** A key strictly between a and b; null means before everything / after everything. */
export function keyBetween(a: string | null, b: string | null): string {
  if (a !== null) validate(a);
  if (b !== null) validate(b);
  if (a !== null && b !== null && a >= b) throw new Error(`${a} >= ${b}`);
  if (a === null) {
    if (b === null) return 'a' + DIGITS[0];
    const ib = integerPart(b);
    const fb = b.slice(ib.length);
    if (ib === SMALLEST_INTEGER) return ib + midpoint('', fb);
    if (ib < b) return ib;
    const res = decrement(ib);
    if (res === null) throw new Error('cannot decrement any more');
    return res;
  }
  if (b === null) {
    const ia = integerPart(a);
    const fa = a.slice(ia.length);
    const i = increment(ia);
    return i === null ? ia + midpoint(fa, null) : i;
  }
  const ia = integerPart(a);
  const fa = a.slice(ia.length);
  const ib = integerPart(b);
  const fb = b.slice(ib.length);
  if (ia === ib) return ia + midpoint(fa, fb);
  const i = increment(ia);
  if (i === null) throw new Error('cannot increment any more');
  if (i < b) return i;
  return ia + midpoint(fa, null);
}

/** Byte-order comparison matching PostgreSQL COLLATE "C"; ties broken by ID. */
export function compareKeys(a: { position: string; id: string }, b: { position: string; id: string }): number {
  if (a.position !== b.position) return a.position < b.position ? -1 : 1;
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}
