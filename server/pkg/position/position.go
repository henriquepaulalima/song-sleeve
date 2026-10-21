// Package position generates lexicographic fractional order keys (R-ORG-6), so a move
// writes a single row. It is a port of the "fractional-indexing" algorithm and must
// stay identical to packages/core/src/position.ts: server and device overlay items
// share one key space. Keys compare with byte ordering (COLLATE "C").
package position

import (
	"errors"
	"strings"
)

const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var smallestInteger = "A" + strings.Repeat("0", 26)

// Between returns a key strictly between a and b. An empty a means "before
// everything"; an empty b means "after everything".
func Between(a, b string) (string, error) {
	if a != "" {
		if err := validate(a); err != nil {
			return "", err
		}
	}
	if b != "" {
		if err := validate(b); err != nil {
			return "", err
		}
	}
	if a != "" && b != "" && a >= b {
		return "", errors.New("position: a >= b")
	}
	if a == "" {
		if b == "" {
			return "a" + string(digits[0]), nil
		}
		ib := integerPart(b)
		fb := b[len(ib):]
		if ib == smallestInteger {
			m, err := midpoint("", fb, true)
			return ib + m, err
		}
		if ib < b {
			return ib, nil
		}
		res, ok := decrement(ib)
		if !ok {
			return "", errors.New("position: cannot decrement")
		}
		return res, nil
	}
	if b == "" {
		ia := integerPart(a)
		fa := a[len(ia):]
		if i, ok := increment(ia); ok {
			return i, nil
		}
		m, err := midpoint(fa, "", false)
		return ia + m, err
	}
	ia := integerPart(a)
	fa := a[len(ia):]
	ib := integerPart(b)
	fb := b[len(ib):]
	if ia == ib {
		m, err := midpoint(fa, fb, true)
		return ia + m, err
	}
	i, ok := increment(ia)
	if !ok {
		return "", errors.New("position: cannot increment")
	}
	if i < b {
		return i, nil
	}
	m, err := midpoint(fa, "", false)
	return ia + m, err
}

// After returns a key after a ("" for the first key).
func After(a string) string {
	k, err := Between(a, "")
	if err != nil {
		panic(err)
	}
	return k
}

func midpoint(a, b string, hasB bool) (string, error) {
	zero := digits[0]
	if hasB && a >= b {
		return "", errors.New("position: midpoint a >= b")
	}
	if (a != "" && a[len(a)-1] == zero) || (hasB && b != "" && b[len(b)-1] == zero) {
		return "", errors.New("position: trailing zero")
	}
	if hasB && b != "" {
		n := 0
		for n < len(b) {
			ca := zero
			if n < len(a) {
				ca = a[n]
			}
			if ca != b[n] {
				break
			}
			n++
		}
		if n > 0 {
			rest := ""
			if n < len(a) {
				rest = a[n:]
			}
			m, err := midpoint(rest, b[n:], true)
			return b[:n] + m, err
		}
	}
	digitA := 0
	if a != "" {
		digitA = strings.IndexByte(digits, a[0])
	}
	digitB := len(digits)
	if hasB && b != "" {
		digitB = strings.IndexByte(digits, b[0])
	}
	if digitB-digitA > 1 {
		mid := (digitA + digitB + 1) / 2 // Math.round(0.5 * (a + b)) for non-negative ints
		return string(digits[mid]), nil
	}
	if hasB && len(b) > 1 {
		return b[:1], nil
	}
	rest := ""
	if len(a) > 1 {
		rest = a[1:]
	}
	m, err := midpoint(rest, "", false)
	return string(digits[digitA]) + m, err
}

func integerLength(head byte) int {
	switch {
	case head >= 'a' && head <= 'z':
		return int(head-'a') + 2
	case head >= 'A' && head <= 'Z':
		return int('Z'-head) + 2
	}
	return -1
}

func integerPart(key string) string {
	n := integerLength(key[0])
	if n < 0 || n > len(key) {
		return key
	}
	return key[:n]
}

func validate(key string) error {
	if key == smallestInteger {
		return errors.New("position: invalid key")
	}
	n := integerLength(key[0])
	if n < 0 || n > len(key) {
		return errors.New("position: invalid integer part")
	}
	for i := 0; i < len(key); i++ {
		if strings.IndexByte(digits, key[i]) < 0 {
			return errors.New("position: invalid character")
		}
	}
	f := key[n:]
	if f != "" && f[len(f)-1] == digits[0] {
		return errors.New("position: trailing zero")
	}
	return nil
}

func increment(x string) (string, bool) {
	head := x[0]
	digs := []byte(x[1:])
	carry := true
	for i := len(digs) - 1; carry && i >= 0; i-- {
		d := strings.IndexByte(digits, digs[i]) + 1
		if d == len(digits) {
			digs[i] = digits[0]
		} else {
			digs[i] = digits[d]
			carry = false
		}
	}
	if carry {
		if head == 'Z' {
			return "a" + string(digits[0]), true
		}
		if head == 'z' {
			return "", false
		}
		h := head + 1
		if h > 'a' {
			digs = append(digs, digits[0])
		} else {
			digs = digs[:len(digs)-1]
		}
		return string(h) + string(digs), true
	}
	return string(head) + string(digs), true
}

func decrement(x string) (string, bool) {
	head := x[0]
	digs := []byte(x[1:])
	borrow := true
	for i := len(digs) - 1; borrow && i >= 0; i-- {
		d := strings.IndexByte(digits, digs[i]) - 1
		if d == -1 {
			digs[i] = digits[len(digits)-1]
		} else {
			digs[i] = digits[d]
			borrow = false
		}
	}
	if borrow {
		if head == 'a' {
			return "Z" + string(digits[len(digits)-1]), true
		}
		if head == 'A' {
			return "", false
		}
		h := head - 1
		if h < 'Z' {
			digs = append(digs, digits[len(digits)-1])
		} else {
			digs = digs[:len(digs)-1]
		}
		return string(h) + string(digs), true
	}
	return string(head) + string(digs), true
}
