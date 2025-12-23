package cache

import (
	"net"
	"sync"
	"fmt"
)

// Pub/Sub
func (c *BoltCache) Subscribe(channel string, conn net.Conn) {
	conns, _ := c.Subscribers.LoadOrStore(channel, &sync.Map{})
	conns.(*sync.Map).Store(conn, true)
}

func (c *BoltCache) Publish(channel string, message string) int {
	count := 0
	if conns, ok := c.Subscribers.Load(channel); ok {
		conns.(*sync.Map).Range(func(key, value interface{}) bool {
			conn := key.(net.Conn)
			fmt.Fprintf(conn, "MESSAGE %s %s\n", channel, message)
			count++
			return true
		})
	}
	return count
}