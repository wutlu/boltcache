package command

import (
	"github.com/wutlu/boltcache/internal/cache"
	"github.com/wutlu/boltcache/internal/resp"
)

func registerTxCommands() {
	const ctl = flagMultiControl | flagNoScript | flagFast
	register("multi", cmdMulti, 1, ctl, 0, 0, 0)
	register("exec", cmdExec, 1, ctl, 0, 0, 0)
	register("discard", cmdDiscard, 1, ctl, 0, 0, 0)
	register("watch", cmdWatch, -2, ctl, 1, -1, 1).flags |= flagNoKeys
	register("unwatch", cmdUnwatch, 1, ctl, 0, 0, 0)
}

func cmdMulti(_ *Engine, s *Session, _ *cache.Tx, _ []string, w resp.Writer) {
	if s.inMulti {
		w.Error("ERR MULTI calls can not be nested")
		return
	}
	s.inMulti, s.multiAborted, s.queue = true, false, nil
	w.Simple("OK")
}

func cmdDiscard(_ *Engine, s *Session, _ *cache.Tx, _ []string, w resp.Writer) {
	if !s.inMulti {
		w.Error("ERR DISCARD without MULTI")
		return
	}
	s.inMulti, s.multiAborted, s.queue, s.watched = false, false, nil, nil
	w.Simple("OK")
}

// WATCH is implemented with per-shard write counters: EXEC aborts if any
// shard holding a watched key was written since WATCH. This can abort
// spuriously when an unrelated key in the same shard changes (clients retry
// such transactions anyway), but never misses a real modification.
func cmdWatch(e *Engine, s *Session, _ *cache.Tx, args []string, w resp.Writer) {
	if s.inMulti {
		w.Error("ERR WATCH inside MULTI is not allowed")
		return
	}
	if s.watched == nil {
		s.watched = map[string]uint64{}
	}
	for _, k := range args[1:] {
		if _, ok := s.watched[k]; !ok {
			s.watched[k] = e.cache.KeyVersion(k)
		}
	}
	w.Simple("OK")
}

func cmdUnwatch(_ *Engine, s *Session, _ *cache.Tx, _ []string, w resp.Writer) {
	s.watched = nil
	w.Simple("OK")
}

func cmdExec(e *Engine, s *Session, _ *cache.Tx, _ []string, w resp.Writer) {
	if !s.inMulti {
		w.Error("ERR EXEC without MULTI")
		return
	}
	queue, aborted, watched := s.queue, s.multiAborted, s.watched
	s.inMulti, s.multiAborted, s.queue, s.watched = false, false, nil, nil
	if aborted {
		w.Error("EXECABORT Transaction discarded because of previous errors.")
		return
	}

	specs := make([]*spec, len(queue))
	all := false
	var keys []string
	for i, q := range queue {
		sp := lookup(q[0])
		specs[i] = sp
		if sp.flags&flagDenyOOM != 0 && e.cache.OOM() {
			w.Error("EXECABORT Transaction discarded: " + cache.ErrOOM.Error())
			return
		}
		if sp.flags&flagGlobal != 0 {
			all = true
		}
		if sp.flags&flagNoTx == 0 {
			keys = append(keys, sp.keys(q)...)
		}
	}
	for k := range watched {
		keys = append(keys, k)
	}

	replies := make([]resp.Value, len(queue))
	watchFailed := false
	_ = e.cache.Atomic(keys, all, func(tx *cache.Tx) error {
		for k, v := range watched {
			if e.cache.KeyVersion(k) != v {
				watchFailed = true
				return nil
			}
		}
		s.tx = tx
		defer func() { s.tx = nil }()
		for i, q := range queue {
			if specs[i].flags&flagNoTx != 0 {
				continue
			}
			var rec resp.Recorder
			e.run(s, specs[i], q, &rec)
			replies[i] = rec.Result()
		}
		return nil
	})
	if watchFailed {
		w.NullArray()
		return
	}
	// Commands that can't run under shard locks (INFO, CONFIG...) run
	// afterwards; their replies keep their position.
	for i, q := range queue {
		if specs[i].flags&flagNoTx == 0 {
			continue
		}
		var rec resp.Recorder
		e.run(s, specs[i], q, &rec)
		replies[i] = rec.Result()
	}
	w.Array(len(replies))
	for _, r := range replies {
		r.Write(w)
	}
}
