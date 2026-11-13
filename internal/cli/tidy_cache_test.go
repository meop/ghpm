package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
)

var cacheEntryN int

func makeCacheEntry(t *testing.T, age time.Duration) string {
	t.Helper()
	cacheDir, err := store.HTTPCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	cacheEntryN++
	// the client's cache files sit in a two-level hash fanout
	p := filepath.Join(cacheDir, fmt.Sprintf("ab%02d", cacheEntryN), "cd", "ef")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("api answer"), 0644); err != nil {
		t.Fatal(err)
	}
	if age > 0 {
		stamp := time.Now().Add(-age)
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestCleanStaleHTTPCache_RemovesStaleKeepsFresh(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	stale := makeCacheEntry(t, time.Hour)
	fresh := makeCacheEntry(t, 0)

	if !cleanStaleHTTPCache(&config.Settings{CacheTTL: "5m"}) {
		t.Error("tidy did not report anything cleaned")
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Error("stale cache entry was not removed")
	}
	if _, err := os.Lstat(fresh); err != nil {
		t.Error("fresh cache entry was removed")
	}
	// the fanout dirs around a removed entry go with it
	if _, err := os.Lstat(filepath.Dir(stale)); !os.IsNotExist(err) {
		t.Error("empty fanout dirs were not pruned")
	}
}

func TestCleanStaleHTTPCache_KeepsEverythingFresh(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	fresh := makeCacheEntry(t, 0)

	if cleanStaleHTTPCache(&config.Settings{CacheTTL: "5m"}) {
		t.Error("tidy reported work on a clean cache")
	}
	if _, err := os.Lstat(fresh); err != nil {
		t.Error("fresh cache entry was removed")
	}
}

func TestCleanStaleHTTPCache_BadTTLLeavesCacheAlone(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	stale := makeCacheEntry(t, time.Hour)

	if cleanStaleHTTPCache(&config.Settings{CacheTTL: "soon"}) {
		t.Error("tidy reported work with an unreadable ttl")
	}
	if _, err := os.Lstat(stale); err != nil {
		t.Error("stale cache entry was removed without a readable ttl")
	}
}
