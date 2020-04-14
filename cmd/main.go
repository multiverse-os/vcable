package main

import (
	"fmt"

	framework "../framework"
)

func main() {
	fmt.Println("vcable")
	fmt.Println("===================")

	cable := framework.NewCable()

	cable.Connect()
}
