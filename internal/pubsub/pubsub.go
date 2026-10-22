// Package pubsub implements channel and pattern based publish/subscribe.
//
// Publishing never blocks on subscribers: each Subscriber decides how to
// deliver (typically by queueing to a per-connection writer) and slow
// subscribers are dropped by their transport rather than stalling
// publishers.
package pubsub

import (
	"sort"
	"sync"

	"github.com/wutlu/boltcache/internal/glob"
)

// Subscriber receives published messages. Deliver must not block.
// pattern is empty for messages received through a channel subscription.
type Subscriber interface {
	Deliver(channel, pattern, message string)
}

// Hub routes messages from publishers to subscribers.
type Hub struct {
	mu       sync.RWMutex
	channels map[string]map[Subscriber]struct{}
	patterns map[string]map[Subscriber]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{
		channels: make(map[string]map[Subscriber]struct{}),
		patterns: make(map[string]map[Subscriber]struct{}),
	}
}

func add(m map[string]map[Subscriber]struct{}, key string, s Subscriber) bool {
	subs, ok := m[key]
	if !ok {
		subs = make(map[Subscriber]struct{})
		m[key] = subs
	}
	if _, dup := subs[s]; dup {
		return false
	}
	subs[s] = struct{}{}
	return true
}

func remove(m map[string]map[Subscriber]struct{}, key string, s Subscriber) bool {
	subs, ok := m[key]
	if !ok {
		return false
	}
	if _, ok := subs[s]; !ok {
		return false
	}
	delete(subs, s)
	if len(subs) == 0 {
		delete(m, key)
	}
	return true
}

// Subscribe adds s to channel. It reports whether s was newly subscribed.
func (h *Hub) Subscribe(s Subscriber, channel string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return add(h.channels, channel, s)
}

// Unsubscribe removes s from channel.
func (h *Hub) Unsubscribe(s Subscriber, channel string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return remove(h.channels, channel, s)
}

// PSubscribe adds s to a glob pattern.
func (h *Hub) PSubscribe(s Subscriber, pattern string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return add(h.patterns, pattern, s)
}

// PUnsubscribe removes s from a pattern.
func (h *Hub) PUnsubscribe(s Subscriber, pattern string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return remove(h.patterns, pattern, s)
}

// UnsubscribeAll removes every subscription of s, e.g. when its connection
// closes.
func (h *Hub) UnsubscribeAll(s Subscriber, channels, patterns []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range channels {
		remove(h.channels, c, s)
	}
	for _, p := range patterns {
		remove(h.patterns, p, s)
	}
}

// Publish delivers message to subscribers of channel and of matching
// patterns, returning the number of deliveries.
func (h *Hub) Publish(channel, message string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for s := range h.channels[channel] {
		s.Deliver(channel, "", message)
		n++
	}
	for p, subs := range h.patterns {
		if !glob.Match(p, channel) {
			continue
		}
		for s := range subs {
			s.Deliver(channel, p, message)
			n++
		}
	}
	return n
}

// Channels returns active channels matching pattern ("" means all), sorted.
func (h *Hub) Channels(pattern string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := []string{}
	for c := range h.channels {
		if pattern == "" || glob.Match(pattern, c) {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// NumSub returns the subscriber count of each channel.
func (h *Hub) NumSub(channels ...string) []int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]int, len(channels))
	for i, c := range channels {
		out[i] = len(h.channels[c])
	}
	return out
}

// NumPat returns the number of distinct pattern subscriptions.
func (h *Hub) NumPat() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, subs := range h.patterns {
		n += len(subs)
	}
	return n
}
