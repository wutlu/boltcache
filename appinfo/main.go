// Package appinfo holds build and version information.
package appinfo

import (
	"regexp"
	"runtime/debug"
	"time"
)

var (
	Name        = "boltcache"
	Title       = "BoltCache"
	Description = "A lightweight, high-performance in-memory caching server written in Go."

	// Version, Commit and Date are set at build time with
	//   -ldflags "-X github.com/wutlu/boltcache/appinfo.Version=2.0.0 ..."
	Version = "2.0.0"
	Commit  = ""
	Date    = ""

	StartTime = time.Now()
)

func init() {
	// `go install github.com/wutlu/boltcache@vX` builds carry the module
	// version and VCS info in the binary.
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	// Only trust release versions: local builds get VCS pseudo-versions
	// such as v1.0.1-0.20251227160035-e6a00dc50be8+dirty.
	if v := info.Main.Version; releaseVersion.MatchString(v) && Commit == "" {
		Version = trimV(v)
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if Commit == "" {
				Commit = s.Value
			}
		case "vcs.time":
			if Date == "" {
				Date = s.Value
			}
		}
	}
}

var releaseVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)

func trimV(v string) string {
	if len(v) > 1 && v[0] == 'v' {
		return v[1:]
	}
	return v
}
