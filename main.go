package main

import (
	"github.com/wutlu/boltcache/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}
