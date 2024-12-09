// Copyright 2020 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package bls12381

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
)

// fe is base field element representation
type fe [6]uint64

// fe2 is element representation of 'fp2' which is quadratic extension of base field 'fp'
// Representation follows c[0] + c[1] * u encoding order.
type fe2 [2]fe

// fe6 is element representation of 'fp6' field which is cubic extension of 'fp2'
// Representation follows c[0] + c[1] * v + c[2] * v^2 encoding order.
type fe6 [3]fe2

// fe12 is element representation of 'fp12' field which is quadratic extension of 'fp6'
// Representation follows c[0] + c[1] * w encoding order.
type fe12 [2]fe6

func (f *fe) setBytes(in []byte) *fe {
	size := 48
	l := len(in)
	if l >= size {
		l = size
	}
	padded := make([]byte, size)
	copy(padded[size-l:], in[:])
	var a int
	for i := 0; i < 6; i++ {
		a = size - i*8
		f[i] = uint64(padded[a-1]) | uint64(padded[a-2])<<8 |
			uint64(padded[a-3])<<16 | uint64(padded[a-4])<<24 |
			uint64(padded[a-5])<<32 | uint64(padded[a-6])<<40 |
			uint64(padded[a-7])<<48 | uint64(padded[a-8])<<56
	}
	return f
}

func (f *fe) setBig(a *big.Int) *fe {
	return f.setBytes(a.Bytes())
}

func (f *fe) setString(s string) (*fe, error) {
	if s[:2] == "0x" {
		s = s[2:]
	}
	bytes, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return f.setBytes(bytes), nil
}

func (f *fe) set(fe2 *fe) *fe {
	f[0] = fe2[0]
	f[1] = fe2[1]
	f[2] = fe2[2]
	f[3] = fe2[3]
	f[4] = fe2[4]
	f[5] = fe2[5]
	return f
}

func (f *fe) bytes() []byte {
	out := make([]byte, 48)
	var a int
	for i := 0; i < 6; i++ {
		a = 48 - i*8
		out[a-1] = byte(f[i])
		out[a-2] = byte(f[i] >> 8)
		out[a-3] = byte(f[i] >> 16)
		out[a-4] = byte(f[i] >> 24)
		out[a-5] = byte(f[i] >> 32)
		out[a-6] = byte(f[i] >> 40)
		out[a-7] = byte(f[i] >> 48)
		out[a-8] = byte(f[i] >> 56)
	}
	return out
}

func (f *fe) big() *big.Int {
	return new(big.Int).SetBytes(f.bytes())
}

func (f *fe) string() (s string) {
	for i := 5; i >= 0; i-- {
		s = fmt.Sprintf("%s%16.16x", s, f[i])
	}
	return "0x" + s
}

func (f *fe) zero() *fe {
	f[0] = 0
	f[1] = 0
	f[2] = 0
	f[3] = 0
	f[4] = 0
	f[5] = 0
	return f
}

func (f *fe) one() *fe {
	return f.set(r1)
}

func (f *fe) rand(r io.Reader) (*fe, error) {
	bi, err := rand.Int(r, modulus.big())
	if err != nil {
		return nil, err
	}
	return f.setBig(bi), nil
}

func (f *fe) isValid() bool {
	return f.cmp(&modulus) < 0
}

func (f *fe) isOdd() bool {
	var mask uint64 = 1
	return f[0]&mask != 0
}

func (f *fe) isEven() bool {
	var mask uint64 = 1
	return f[0]&mask == 0
}

func (f *fe) isZero() bool {
	return (f[5] | f[4] | f[3] | f[2] | f[1] | f[0]) == 0
}

func (f *fe) isOne() bool {
	return f.equal(r1)
}

func (f *fe) cmp(fe2 *fe) int {
	for i := 5; i >= 0; i-- {
		if f[i] > fe2[i] {
			return 1
		} else if f[i] < fe2[i] {
			return -1
		}
	}
	return 0
}

func (f *fe) equal(fe2 *fe) bool {
	return fe2[0] == f[0] && fe2[1] == f[1] && fe2[2] == f[2] && fe2[3] == f[3] && fe2[4] == f[4] && fe2[5] == f[5]
}

func (f *fe) sign() bool {
	r := new(fe)
	fromMont(r, f)
	return r[0]&1 == 0
}

func (f *fe) div2(e uint64) {
	f[0] = f[0]>>1 | f[1]<<63
	f[1] = f[1]>>1 | f[2]<<63
	f[2] = f[2]>>1 | f[3]<<63
	f[3] = f[3]>>1 | f[4]<<63
	f[4] = f[4]>>1 | f[5]<<63
	f[5] = f[5]>>1 | e<<63
}

func (f *fe) mul2() uint64 {
	e := f[5] >> 63
	f[5] = f[5]<<1 | f[4]>>63
	f[4] = f[4]<<1 | f[3]>>63
	f[3] = f[3]<<1 | f[2]>>63
	f[2] = f[2]<<1 | f[1]>>63
	f[1] = f[1]<<1 | f[0]>>63
	f[0] = f[0] << 1
	return e
}

func (e *fe2) zero() *fe2 {
	e[0].zero()
	e[1].zero()
	return e
}

func (e *fe2) one() *fe2 {
	e[0].one()
	e[1].zero()
	return e
}

func (e *fe2) set(e2 *fe2) *fe2 {
	e[0].set(&e2[0])
	e[1].set(&e2[1])
	return e
}

func (e *fe2) rand(r io.Reader) (*fe2, error) {
	a0, err := new(fe).rand(r)
	if err != nil {
		return nil, err
	}
	a1, err := new(fe).rand(r)
	if err != nil {
		return nil, err
	}
	return &fe2{*a0, *a1}, nil
}

func (e *fe2) isOne() bool {
	return e[0].isOne() && e[1].isZero()
}

func (e *fe2) isZero() bool {
	return e[0].isZero() && e[1].isZero()
}

func (e *fe2) equal(e2 *fe2) bool {
	return e[0].equal(&e2[0]) && e[1].equal(&e2[1])
}

func (e *fe2) sign() bool {
	r := new(fe)
	if !e[0].isZero() {
		fromMont(r, &e[0])
		return r[0]&1 == 0
	}
	fromMont(r, &e[1])
	return r[0]&1 == 0
}

func (e *fe6) zero() *fe6 {
	e[0].zero()
	e[1].zero()
	e[2].zero()
	return e
}

func (e *fe6) one() *fe6 {
	e[0].one()
	e[1].zero()
	e[2].zero()
	return e
}

func (e *fe6) set(e2 *fe6) *fe6 {
	e[0].set(&e2[0])
	e[1].set(&e2[1])
	e[2].set(&e2[2])
	return e
}

func (e *fe6) rand(r io.Reader) (*fe6, error) {
	a0, err := new(fe2).rand(r)
	if err != nil {
		return nil, err
	}
	a1, err := new(fe2).rand(r)
	if err != nil {
		return nil, err
	}
	a2, err := new(fe2).rand(r)
	if err != nil {
		return nil, err
	}
	return &fe6{*a0, *a1, *a2}, nil
}

func (e *fe6) isOne() bool {
	return e[0].isOne() && e[1].isZero() && e[2].isZero()
}

func (e *fe6) isZero() bool {
	return e[0].isZero() && e[1].isZero() && e[2].isZero()
}

func (e *fe6) equal(e2 *fe6) bool {
	return e[0].equal(&e2[0]) && e[1].equal(&e2[1]) && e[2].equal(&e2[2])
}

func (e *fe12) zero() *fe12 {
	e[0].zero()
	e[1].zero()
	return e
}

func (e *fe12) one() *fe12 {
	e[0].one()
	e[1].zero()
	return e
}

func (e *fe12) set(e2 *fe12) *fe12 {
	e[0].set(&e2[0])
	e[1].set(&e2[1])
	return e
}

func (e *fe12) rand(r io.Reader) (*fe12, error) {
	a0, err := new(fe6).rand(r)
	if err != nil {
		return nil, err
	}
	a1, err := new(fe6).rand(r)
	if err != nil {
		return nil, err
	}
	return &fe12{*a0, *a1}, nil
}

func (e *fe12) isOne() bool {
	return e[0].isOne() && e[1].isZero()
}

func (e *fe12) isZero() bool {
	return e[0].isZero() && e[1].isZero()
}

func (e *fe12) equal(e2 *fe12) bool {
	return e[0].equal(&e2[0]) && e[1].equal(&e2[1])
}
