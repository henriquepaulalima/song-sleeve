import { COMMON_LONG_PASSWORDS } from './common';

export const PASSWORD_MIN = 10;
export const PASSWORD_MAX = 128;

export interface PasswordRules {
  length: boolean;
  notCommon: boolean;
  valid: boolean;
}

/** Live check of the password policy (R-AUTH-9): 10–128 characters, any kind, not common. */
export function checkPassword(password: string): PasswordRules {
  const pw = password.normalize('NFC');
  const n = [...pw].length;
  const length = n >= PASSWORD_MIN && n <= PASSWORD_MAX;
  const notCommon = n > 0 && !COMMON_LONG_PASSWORDS.has(pw.toLowerCase());
  return { length, notCommon, valid: length && notCommon };
}
