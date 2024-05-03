// Copyright 2015 The go-ethereum Authors
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

package common

type Signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type Unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type Integer interface {
	Signed | Unsigned
}

type Float interface {
	~float32 | ~float64
}

// Ordered is anything that implements comparison operators such as `<` and `>`.
// Unfortunately, that doesn't include big ints.
type Ordered interface {
	Integer | Float
}

// MinInt the minimum of two ints
func MinInt[T Ordered](value, ceiling T) T {
	if value > ceiling {
		return ceiling
	}
	return value
}

// MaxInt the maximum of two ints
func MaxInt[T Ordered](value, floor T) T {
	if value < floor {
		return floor
	}
	return value
}

// SaturatingUAdd add two integers without overflow
func SaturatingUAdd[T Unsigned](a, b T) T {
	sum := a + b
	if sum < a || sum < b {
		sum = ^T(0)
	}
	return sum
}
