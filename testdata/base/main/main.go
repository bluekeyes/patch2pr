package main

import (
	"fmt"
	"math/rand"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func main() {
	x := rand.Uint64()
	x = Fiddle(x)
	x = Twist(x)

	y := rand.Uint64()
	r := Rotate(y, rand.Intn(64))

	v := Collide(r, Collide(x, y))
	fmt.Printf("%x\n", v)
}
