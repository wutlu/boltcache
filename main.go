package main

import (
	"boltcache/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}
