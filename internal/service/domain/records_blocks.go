package domain

import "github.com/namecheap/go-namecheap-sdk/v2/namecheap"

// defaultTTL and defaultMXPref are the values Namecheap assumes for a host
// record when the caller omits them. They are applied in code rather than
// through the schema defaults layer, which reaches top-level fields only.
const (
	defaultTTL    = 1800
	defaultMXPref = 10
)

// Record is one host DNS record managed within a domain-records resource. The
// hostname is the record's label ("@" for the apex, "www", and so on); the
// address is the value, whose meaning follows the type (an IP for A, a target
// for CNAME, a URL for URL, and the like). The MX preference applies to MX
// records and the TTL to every type; both fall back to Namecheap's defaults
// when omitted.
type Record struct {
	Hostname string `ub:"hostname"`
	Type     string `ub:"type"`
	Address  string `ub:"address"`
	MXPref   *int64 `ub:"mx-pref"`
	TTL      *int64 `ub:"ttl"`
}

// hostRecord converts the block into the SDK host record, filling in the TTL
// and MX-preference defaults Namecheap assumes when they are omitted. The
// address is sent verbatim; the trailing-dot and CAA-quote canonicalization
// applies only to the hashing that pairs a desired record with its remote twin.
func (r Record) hostRecord() namecheap.DomainsDNSHostRecord {
	ttl := defaultTTL
	if r.TTL != nil {
		ttl = int(*r.TTL)
	}
	mxPref := uint8(defaultMXPref)
	if r.MXPref != nil {
		mxPref = uint8(*r.MXPref)
	}
	return namecheap.DomainsDNSHostRecord{
		HostName:   new(r.Hostname),
		RecordType: new(r.Type),
		Address:    new(r.Address),
		MXPref:     new(mxPref),
		TTL:        new(ttl),
	}
}

// RecordOutput identifies one managed host record in a domain-records output by
// its hostname, type, and address. The next apply hashes the three to pair the
// record with its remote twin, and a merge-mode Delete addresses them to remove
// exactly the records this resource owned.
type RecordOutput struct {
	Hostname string `ub:"hostname"`
	Type     string `ub:"type"`
	Address  string `ub:"address"`
}
