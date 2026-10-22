package main

import (
	"github.com/wutlu/boltcache/v2/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}
