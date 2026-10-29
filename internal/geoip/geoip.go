// Package geoip performs offline, bounded-cache lookups in local MaxMind files.
package geoip

import (
	"container/list"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/oschwald/maxminddb-golang/v2"
)

const CacheEntries = 2048

type Info struct {
	CountryCode, CountryName, Organization string
	ASN                                    uint32
}

func (i Info) Available() bool { return i.CountryCode != "" || i.ASN != 0 }

type entry struct {
	ip   netip.Addr
	info Info
}
type Resolver struct {
	mu           sync.Mutex
	country, asn *maxminddb.Reader
	cache        map[netip.Addr]*list.Element
	order        *list.List
	lookup       func(netip.Addr) (Info, error)
	closed       bool
}

// Open treats an absent pair as optional, allowing dashboards without GeoIP.
// A partial pair or invalid file is an error rather than a misleading report.
func Open(dir string) (*Resolver, error) {
	if dir == "" {
		return nil, nil
	}
	countryPath, asnPath := filepath.Join(dir, "GeoLite2-Country.mmdb"), filepath.Join(dir, "GeoLite2-ASN.mmdb")
	_, cErr := os.Stat(countryPath)
	_, aErr := os.Stat(asnPath)
	if errors.Is(cErr, os.ErrNotExist) && errors.Is(aErr, os.ErrNotExist) {
		return nil, nil
	}
	if cErr != nil {
		return nil, fmt.Errorf("country database: %w", cErr)
	}
	if aErr != nil {
		return nil, fmt.Errorf("ASN database: %w", aErr)
	}
	country, err := maxminddb.Open(countryPath, maxminddb.DisableStringCache())
	if err != nil {
		return nil, err
	}
	asn, err := maxminddb.Open(asnPath, maxminddb.DisableStringCache())
	if err != nil {
		country.Close()
		return nil, err
	}
	if !strings.HasSuffix(country.Metadata.DatabaseType, "-Country") || !strings.HasSuffix(asn.Metadata.DatabaseType, "-ASN") {
		country.Close()
		asn.Close()
		return nil, errors.New("GeoIP files must contain Country and ASN databases")
	}
	r := &Resolver{country: country, asn: asn, cache: map[netip.Addr]*list.Element{}, order: list.New()}
	r.lookup = r.read
	return r, nil
}

func (r *Resolver) read(ip netip.Addr) (Info, error) {
	var c struct {
		Country struct {
			Code  string `maxminddb:"iso_code,maxsize:4"`
			Names struct {
				English string `maxminddb:"en,maxsize:128"`
			} `maxminddb:"names"`
		} `maxminddb:"country"`
	}
	var a struct {
		Number       uint32 `maxminddb:"autonomous_system_number"`
		Organization string `maxminddb:"autonomous_system_organization,maxsize:256"`
	}
	if err := r.country.Lookup(ip).Decode(&c); err != nil {
		return Info{}, err
	}
	if err := r.asn.Lookup(ip).Decode(&a); err != nil {
		return Info{}, err
	}
	return Info{CountryCode: c.Country.Code, CountryName: c.Country.Names.English, ASN: a.Number, Organization: a.Organization}, nil
}

// Lookup does not resolve DNS or contact any API. Readers stay open for the
// process lifetime; only requested player IPs are decoded. Misses are cached too.
func (r *Resolver) Lookup(address string) (Info, error) {
	if r == nil {
		return Info{}, nil
	}
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return Info{}, nil
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return Info{}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Info{}, errors.New("GeoIP resolver is closed")
	}
	if e := r.cache[ip]; e != nil {
		r.order.MoveToFront(e)
		return e.Value.(entry).info, nil
	}
	info, err := r.lookup(ip)
	if err != nil {
		return Info{}, err
	}
	if len(r.cache) >= CacheEntries {
		old := r.order.Back()
		delete(r.cache, old.Value.(entry).ip)
		r.order.Remove(old)
	}
	r.cache[ip] = r.order.PushFront(entry{ip: ip, info: info})
	return info, nil
}

func (r *Resolver) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var errs []error
	if r.country != nil {
		errs = append(errs, r.country.Close())
	}
	if r.asn != nil {
		errs = append(errs, r.asn.Close())
	}
	return errors.Join(errs...)
}
