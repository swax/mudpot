package geoip

import (
	"container/list"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCacheIsBoundedAndCachesMisses(t *testing.T) {
	calls := 0
	r := &Resolver{cache: map[netip.Addr]*list.Element{}, order: list.New(), lookup: func(ip netip.Addr) (Info, error) {
		calls++
		if ip == netip.MustParseAddr("8.8.8.8") {
			return Info{CountryCode: "US", CountryName: "United States", ASN: 15169, Organization: "Google LLC"}, nil
		}
		return Info{}, nil
	}}
	for i := 0; i < 10; i++ {
		got, err := r.Lookup("::ffff:8.8.8.8")
		if err != nil || got.ASN != 15169 {
			t.Fatal(got, err)
		}
	}
	for i := 0; i < 10; i++ {
		if _, err := r.Lookup("192.0.2.1"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal("successful/negative lookups were not cached", calls)
	}
	for _, ip := range []string{"invalid", "127.0.0.1", "10.0.0.1", "::1", "fe80::1", "224.0.0.1"} {
		r.Lookup(ip)
	}
	if calls != 2 {
		t.Fatal("local/invalid address hit the database")
	}
	for i := 0; i < CacheEntries+50; i++ {
		r.Lookup(fmt.Sprintf("198.18.%d.%d", i/256, i%256))
	}
	if len(r.cache) != CacheEntries || r.order.Len() != CacheEntries {
		t.Fatal("cache exceeded bound")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 50; j++ {
				if _, err := r.Lookup("8.8.8.8"); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	r.Close()
	if _, err := r.Lookup("8.8.8.8"); err == nil {
		t.Fatal("closed reader accepted lookups")
	}
}

func TestOptionalAndInvalidDatabasePair(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil || r != nil {
		t.Fatal(r, err)
	}
	os.WriteFile(filepath.Join(dir, "GeoLite2-Country.mmdb"), []byte("not a database"), 0600)
	if _, err := Open(dir); err == nil {
		t.Fatal("partial pair accepted")
	}
	os.WriteFile(filepath.Join(dir, "GeoLite2-ASN.mmdb"), []byte("not a database"), 0600)
	if _, err := Open(dir); err == nil {
		t.Fatal("corrupt database accepted")
	}
}

func TestLocalMaxMindFiles(t *testing.T) {
	// User-provided licensed data stays gitignored. CI still covers cache,
	// concurrency, invalid input and optional configuration without these files.
	dir := filepath.Join("..", "..", "data", "geoip")
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Skip("local MaxMind databases are not installed")
	}
	defer r.Close()
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"} {
		info, err := r.Lookup(ip)
		if err != nil || info.ASN == 0 || info.Organization == "" {
			t.Fatal(ip, info, err)
		}
		// Anycast networks may have an ASN without an IP-location country.
		// Do not substitute the operator's registered country as a location.
		if ip != "1.1.1.1" && (len(info.CountryCode) != 2 || info.CountryName == "") {
			t.Fatal(ip, info)
		}
	}
}

func BenchmarkLocalLookup(b *testing.B) {
	r, err := Open(filepath.Join("..", "..", "data", "geoip"))
	if err != nil {
		b.Fatal(err)
	}
	if r == nil {
		b.Skip("local MaxMind databases are not installed")
	}
	defer r.Close()
	r.Lookup("8.8.8.8")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := r.Lookup("8.8.8.8"); err != nil {
			b.Fatal(err)
		}
	}
}
