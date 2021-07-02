package main

func Twist(v uint64) uint64 {
	x := 1823 * v
	y := v | (v << 16)
	z := x ^ y
	return z & ((v >> 32) | (v << 32))
}

func Fiddle(v uint64) uint64 {
	v |= 0x80
	v |= 0x8000
	return (v&0xFFFF)<<48 | v&0xFFFFFFFF0000 | (v>>48)&0xFFFF
}

func Collide(a, b uint64) uint64 {
	v := a&(((1<<32)-1)<<32) | (b&(1<<32) - 1)
	v ^= b&(((1<<32)-1)<<32) | (a&(1<<32) - 1)
	return v
}

func Rotate(v uint64, n int) uint64 {
	for i := 0; i < n%64; i++ {
		v = (v&0x1)<<63 | (v >> 1)
	}
	return v
}
