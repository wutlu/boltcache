package command

import (
	"sort"

	"github.com/wutlu/boltcache/v2/internal/cache"
	"github.com/wutlu/boltcache/v2/internal/resp"
)

func registerPubSubCommands() {
	const ps = flagPubSub | flagPubSubCmd | flagNoScript | flagNoTx
	register("subscribe", cmdSubscribe(false), -2, ps, 0, 0, 0)
	register("psubscribe", cmdSubscribe(true), -2, ps, 0, 0, 0)
	register("unsubscribe", cmdUnsubscribe(false), -1, ps, 0, 0, 0)
	register("punsubscribe", cmdUnsubscribe(true), -1, ps, 0, 0, 0)
	register("publish", cmdPublish, 3, flagPubSubCmd|flagFast, 0, 0, 0)
	register("pubsub", cmdPubSub, -2, flagPubSubCmd, 0, 0, 0)
}

func cmdSubscribe(pattern bool) handler {
	return func(e *Engine, s *Session, _ *cache.Tx, args []string, w resp.Writer) {
		if s.Subscriber == nil {
			w.Error("ERR this connection type does not support subscriptions")
			return
		}
		if s.inMulti || s.inScript {
			w.Error("ERR SUBSCRIBE isn't allowed in this context")
			return
		}
		kind := "subscribe"
		if pattern {
			kind = "psubscribe"
		}
		for _, name := range args[1:] {
			if pattern {
				if s.patterns == nil {
					s.patterns = map[string]struct{}{}
				}
				if e.hub.PSubscribe(s.Subscriber, name) {
					s.patterns[name] = struct{}{}
				}
			} else {
				if s.channels == nil {
					s.channels = map[string]struct{}{}
				}
				if e.hub.Subscribe(s.Subscriber, name) {
					s.channels[name] = struct{}{}
				}
			}
			w.Array(3)
			w.Bulk(kind)
			w.Bulk(name)
			w.Int(int64(s.Subscriptions()))
		}
	}
}

func cmdUnsubscribe(pattern bool) handler {
	return func(e *Engine, s *Session, _ *cache.Tx, args []string, w resp.Writer) {
		kind := "unsubscribe"
		set := s.channels
		if pattern {
			kind = "punsubscribe"
			set = s.patterns
		}
		names := args[1:]
		if len(names) == 0 {
			for n := range set {
				names = append(names, n)
			}
			sort.Strings(names)
		}
		if len(names) == 0 {
			w.Array(3)
			w.Bulk(kind)
			w.Null()
			w.Int(int64(s.Subscriptions()))
			return
		}
		for _, name := range names {
			if s.Subscriber != nil {
				if pattern {
					e.hub.PUnsubscribe(s.Subscriber, name)
				} else {
					e.hub.Unsubscribe(s.Subscriber, name)
				}
			}
			delete(set, name)
			w.Array(3)
			w.Bulk(kind)
			w.Bulk(name)
			w.Int(int64(s.Subscriptions()))
		}
	}
}

func cmdPublish(e *Engine, _ *Session, _ *cache.Tx, args []string, w resp.Writer) {
	w.Int(int64(e.hub.Publish(args[1], args[2])))
}

func cmdPubSub(e *Engine, _ *Session, _ *cache.Tx, args []string, w resp.Writer) {
	switch upper(args[1]) {
	case "CHANNELS":
		pattern := ""
		if len(args) > 2 {
			pattern = args[2]
		}
		writeStrings(w, e.hub.Channels(pattern))
	case "NUMSUB":
		counts := e.hub.NumSub(args[2:]...)
		w.Array(2 * len(counts))
		for i, c := range counts {
			w.Bulk(args[2+i])
			w.Int(int64(c))
		}
	case "NUMPAT":
		w.Int(int64(e.hub.NumPat()))
	default:
		w.Error("ERR unknown subcommand '" + args[1] + "'. Try PUBSUB HELP.")
	}
}

// WriteMessage encodes a pub/sub message the way RESP2 subscribers expect.
func WriteMessage(w resp.Writer, channel, pattern, message string) {
	if pattern == "" {
		w.Array(3)
		w.Bulk("message")
		w.Bulk(channel)
		w.Bulk(message)
		return
	}
	w.Array(4)
	w.Bulk("pmessage")
	w.Bulk(pattern)
	w.Bulk(channel)
	w.Bulk(message)
}
