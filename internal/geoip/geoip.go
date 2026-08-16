package geoip

import (
	"net/netip"
	"strings"

	maxminddb "github.com/oschwald/maxminddb-golang/v2"
)

// Result is deliberately reduced to support metadata. The raw address is
// never returned and the prefix is masked before it leaves this package.
type Result struct {
	Prefix      string
	CountryCode string
	CountryName string
}

type record struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
}

// Resolver reads a local MaxMind-compatible database. Keeping the reader
// behind this small type lets deployments replace the database without
// changing the request or persistence paths.
type Resolver struct{ reader *maxminddb.Reader }

func Open(path string) (*Resolver, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	reader, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	return &Resolver{reader: reader}, nil
}

func (r *Resolver) Close() error {
	if r == nil || r.reader == nil {
		return nil
	}
	return r.reader.Close()
}

func (r *Resolver) Lookup(address string) Result {
	if r == nil || r.reader == nil {
		return Result{}
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return Result{}
	}
	result := record{}
	if err := r.reader.Lookup(ip).Decode(&result); err != nil {
		return Result{Prefix: maskedPrefix(ip)}
	}
	return Result{
		Prefix:      maskedPrefix(ip),
		CountryCode: strings.ToUpper(strings.TrimSpace(result.Country.ISOCode)),
		CountryName: strings.TrimSpace(result.Country.Names["en"]),
	}
}

func maskedPrefix(ip netip.Addr) string {
	if ip.Is4() {
		return netip.PrefixFrom(ip, 24).Masked().String()
	}
	return netip.PrefixFrom(ip, 48).Masked().String()
}

// Mask returns only the coarse network prefix used for support context.
func Mask(address string) string {
	ip, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return ""
	}
	return maskedPrefix(ip)
}
