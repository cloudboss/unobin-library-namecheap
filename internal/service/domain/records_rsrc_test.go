package domain

import (
	"context"
	"net/url"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const itDomain = "example.com"

// recordKey projects a record onto its identity for order-independent compares.
func recordKey(hostname, recordType, address string) string {
	return hostname + "|" + recordType + "|" + address
}

func hostKeys(hosts []fakeHost) []string {
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, recordKey(h.name, h.rtype, h.address))
	}
	return out
}

func outputKeys(records []RecordOutput) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, recordKey(r.Hostname, r.Type, r.Address))
	}
	return out
}

func sentKeys(records []sentRecord) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, recordKey(r.Hostname, r.Type, r.Address))
	}
	return out
}

// lastForm returns the most recent request form recorded for a command.
func lastForm(t *testing.T, f *fakeNamecheap, command string) url.Values {
	t.Helper()
	forms := f.sent(command)
	require.NotEmpty(t, forms, "expected at least one %s call", command)
	return forms[len(forms)-1]
}

func TestDomainRecordsCreateOverwrite(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{usingOurDNS: true, emailType: "NONE"})

	r := &DomainRecords{
		Domain: itDomain,
		Mode:   "OVERWRITE",
		Records: []Record{
			{Hostname: "www", Type: "A", Address: "192.0.2.1", TTL: new(int64(300))},
			{Hostname: "txt", Type: "TXT", Address: "hello from unobin"},
		},
	}
	out, err := r.Create(context.Background(), f.configuration())
	require.NoError(t, err)

	// The write replaces the whole set with exactly the two records, each with
	// its TTL and the default MX preference, and an unset email type as NONE.
	form := lastForm(t, f, "namecheap.domains.dns.setHosts")
	assert.Equal(t, "NONE", form.Get("EmailType"))
	assert.ElementsMatch(t, []sentRecord{
		{Hostname: "www", Type: "A", Address: "192.0.2.1", MXPref: "10", TTL: "300"},
		{Hostname: "txt", Type: "TXT", Address: "hello from unobin", MXPref: "10", TTL: "1800"},
	}, recordsFromForm(form))

	assert.Equal(t, itDomain, out.Domain)
	assert.Equal(t, "OVERWRITE", out.Mode)
	assert.Equal(t, "NONE", out.EmailType)
	assert.ElementsMatch(t,
		[]string{recordKey("www", "A", "192.0.2.1"), recordKey("txt", "TXT", "hello from unobin")},
		outputKeys(out.Records))
}

func TestDomainRecordsCreateMergePreservesAndFiltersParking(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: true,
		emailType:   "NONE",
		hosts: []fakeHost{
			{id: 1, name: "www", rtype: "CNAME", address: "parkingpage.namecheap.com.", ttl: 1800},
			{id: 2, name: "blog", rtype: "A", address: "192.0.2.9", mxPref: 10, ttl: 1800},
		},
	})

	r := &DomainRecords{
		Domain:  itDomain,
		Mode:    "MERGE",
		Records: []Record{{Hostname: "mail", Type: "A", Address: "192.0.2.5"}},
	}
	out, err := r.Create(context.Background(), f.configuration())
	require.NoError(t, err)

	// The merge keeps the unmanaged blog record, adds the managed mail record,
	// and drops the parking CNAME.
	assert.ElementsMatch(t, []string{
		recordKey("blog", "A", "192.0.2.9"),
		recordKey("mail", "A", "192.0.2.5"),
	}, sentKeys(recordsFromForm(lastForm(t, f, "namecheap.domains.dns.setHosts"))))

	// The output reports only the record this resource owns.
	assert.ElementsMatch(t, []string{recordKey("mail", "A", "192.0.2.5")}, outputKeys(out.Records))
}

func TestDomainRecordsCreateMergeDuplicate(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: true,
		hosts:       []fakeHost{{id: 1, name: "www", rtype: "A", address: "192.0.2.1", ttl: 1800}},
	})

	r := &DomainRecords{
		Domain:  itDomain,
		Mode:    "MERGE",
		Records: []Record{{Hostname: "www", Type: "A", Address: "192.0.2.1"}},
	}
	_, err := r.Create(context.Background(), f.configuration())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate record")
}

func TestDomainRecordsCreateMergeCanonicalizesCNAME(t *testing.T) {
	f := newFakeNamecheap(t)
	// The remote CNAME carries the trailing dot Namecheap stores.
	f.seed(itDomain, fakeDomain{
		usingOurDNS: true,
		hosts: []fakeHost{
			{id: 1, name: "shop", rtype: "CNAME", address: "shops.example.net.", ttl: 1800},
		},
	})

	// The configured CNAME omits the dot; canonicalization pairs it with the
	// remote record, so the merge reports it as a duplicate.
	r := &DomainRecords{
		Domain:  itDomain,
		Mode:    "MERGE",
		Records: []Record{{Hostname: "shop", Type: "CNAME", Address: "shops.example.net"}},
	}
	_, err := r.Create(context.Background(), f.configuration())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate record")
}

func TestDomainRecordsCreateMergeCanonicalizesCAA(t *testing.T) {
	f := newFakeNamecheap(t)
	// The remote CAA value is quoted as Namecheap stores it.
	f.seed(itDomain, fakeDomain{
		usingOurDNS: true,
		hosts: []fakeHost{
			{id: 1, name: "@", rtype: "CAA", address: `0 issue "letsencrypt.org"`, ttl: 1800},
		},
	})

	// The configured CAA value is unquoted; canonicalization pairs the two.
	r := &DomainRecords{
		Domain:  itDomain,
		Mode:    "MERGE",
		Records: []Record{{Hostname: "@", Type: "CAA", Address: "0 issue letsencrypt.org"}},
	}
	_, err := r.Create(context.Background(), f.configuration())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate record")
}

func TestDomainRecordsCreateSwitchesCustomNSToDefault(t *testing.T) {
	f := newFakeNamecheap(t)
	// The domain is on custom nameservers, which would reject a host-record write.
	f.seed(itDomain, fakeDomain{usingOurDNS: false, nameservers: []string{"a.ns.net", "b.ns.net"}})

	r := &DomainRecords{
		Domain:  itDomain,
		Mode:    "OVERWRITE",
		Records: []Record{{Hostname: "www", Type: "A", Address: "192.0.2.1"}},
	}
	_, err := r.Create(context.Background(), f.configuration())
	require.NoError(t, err)

	assert.NotEmpty(t, f.sent("namecheap.domains.dns.setDefault"),
		"expected a setDefault call to return the domain to Namecheap DNS")
	assert.ElementsMatch(t, []string{recordKey("www", "A", "192.0.2.1")},
		hostKeys(f.state(itDomain).hosts))
}

func TestDomainRecordsCreateOverwriteMX(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{usingOurDNS: true})

	r := &DomainRecords{
		Domain:    itDomain,
		Mode:      "OVERWRITE",
		EmailType: new("MX"),
		Records: []Record{
			{Hostname: "@", Type: "MX", Address: "mail.example.com.", MXPref: new(int64(20))},
		},
	}
	out, err := r.Create(context.Background(), f.configuration())
	require.NoError(t, err)

	form := lastForm(t, f, "namecheap.domains.dns.setHosts")
	assert.Equal(t, "MX", form.Get("EmailType"))
	assert.Equal(t, "20", form.Get("MXPref1"))
	assert.Equal(t, "MX", out.EmailType)
}

func TestDomainRecordsReadOverwriteReturnsAll(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: true,
		emailType:   "NONE",
		hosts: []fakeHost{
			{id: 1, name: "www", rtype: "A", address: "192.0.2.1", ttl: 1800},
			{id: 2, name: "ftp", rtype: "A", address: "192.0.2.2", ttl: 1800},
		},
	})

	r := &DomainRecords{
		Domain:  itDomain,
		Mode:    "OVERWRITE",
		Records: []Record{{Hostname: "www", Type: "A", Address: "192.0.2.1"}},
	}
	out, err := r.Read(context.Background(), f.configuration(), nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		recordKey("www", "A", "192.0.2.1"),
		recordKey("ftp", "A", "192.0.2.2"),
	}, outputKeys(out.Records))
}

func TestDomainRecordsReadMergeReturnsManaged(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: true,
		hosts: []fakeHost{
			{id: 1, name: "www", rtype: "A", address: "192.0.2.1", ttl: 1800},
			{id: 2, name: "ftp", rtype: "A", address: "192.0.2.2", ttl: 1800},
		},
	})

	r := &DomainRecords{
		Domain:  itDomain,
		Mode:    "MERGE",
		Records: []Record{{Hostname: "www", Type: "A", Address: "192.0.2.1"}},
	}
	out, err := r.Read(context.Background(), f.configuration(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{recordKey("www", "A", "192.0.2.1")}, outputKeys(out.Records))
}

func TestDomainRecordsReadMergeUsesPriorOutputRecords(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: true,
		emailType:   "NONE",
		hosts: []fakeHost{
			{
				id:      1,
				name:    "_abc123.www",
				rtype:   "CNAME",
				address: "_x.acm-validations.aws.",
				ttl:     1800,
			},
		},
	})

	r := &DomainRecords{Domain: itDomain, Mode: "MERGE"}
	prior := &DomainRecordsOutput{
		Domain: itDomain,
		Mode:   "MERGE",
		Records: []RecordOutput{
			{
				Hostname: "_abc123.www.example.com.",
				Type:     "CNAME",
				Address:  "_x.acm-validations.aws.",
			},
		},
	}
	out, err := r.Read(context.Background(), f.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, []string{
		recordKey("_abc123.www.example.com.", "CNAME", "_x.acm-validations.aws."),
	}, outputKeys(out.Records))
}

func TestDomainRecordsReadCustomNSNotFound(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{usingOurDNS: false, nameservers: []string{"a.ns.net", "b.ns.net"}})

	r := &DomainRecords{Domain: itDomain, Mode: "MERGE"}
	_, err := r.Read(context.Background(), f.configuration(), nil)
	assert.ErrorIs(t, err, runtime.ErrNotFound)
}

func TestDomainRecordsUpdateMerge(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: true,
		emailType:   "NONE",
		hosts: []fakeHost{
			{id: 1, name: "www", rtype: "A", address: "192.0.2.1", mxPref: 10, ttl: 1800},
			{id: 2, name: "blog", rtype: "A", address: "203.0.113.9", mxPref: 10, ttl: 1800},
		},
	})

	r := &DomainRecords{
		Domain:  itDomain,
		Mode:    "MERGE",
		Records: []Record{{Hostname: "www", Type: "A", Address: "192.0.2.2"}},
	}
	prior := runtime.Prior[DomainRecords, *DomainRecordsOutput]{
		Inputs: DomainRecords{
			Domain:  itDomain,
			Mode:    "MERGE",
			Records: []Record{{Hostname: "www", Type: "A", Address: "192.0.2.1"}},
		},
	}
	_, err := r.Update(context.Background(), f.configuration(), prior)
	require.NoError(t, err)

	// The prior www record gives way to the new one; the unmanaged blog record
	// stays.
	assert.ElementsMatch(t, []string{
		recordKey("blog", "A", "203.0.113.9"),
		recordKey("www", "A", "192.0.2.2"),
	}, hostKeys(f.state(itDomain).hosts))
}

func TestDomainRecordsUpdateOverwrite(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: true,
		hosts:       []fakeHost{{id: 1, name: "old", rtype: "A", address: "192.0.2.1", ttl: 1800}},
	})

	r := &DomainRecords{
		Domain:  itDomain,
		Mode:    "OVERWRITE",
		Records: []Record{{Hostname: "new", Type: "A", Address: "192.0.2.2"}},
	}
	prior := runtime.Prior[DomainRecords, *DomainRecordsOutput]{
		Inputs: DomainRecords{Domain: itDomain, Mode: "OVERWRITE"},
	}
	_, err := r.Update(context.Background(), f.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, []string{recordKey("new", "A", "192.0.2.2")}, hostKeys(f.state(itDomain).hosts))
}

func TestDomainRecordsDeleteOverwrite(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: true,
		hosts:       []fakeHost{{id: 1, name: "www", rtype: "A", address: "192.0.2.1", ttl: 1800}},
	})

	r := &DomainRecords{Domain: itDomain, Mode: "OVERWRITE"}
	prior := &DomainRecordsOutput{
		Domain:  itDomain,
		Mode:    "OVERWRITE",
		Records: []RecordOutput{{Hostname: "www", Type: "A", Address: "192.0.2.1"}},
	}
	require.NoError(t, r.Delete(context.Background(), f.configuration(), prior))
	assert.Empty(t, f.state(itDomain).hosts)
	assert.Equal(t, "NONE", lastForm(t, f, "namecheap.domains.dns.setHosts").Get("EmailType"))
}

func TestDomainRecordsDeleteMerge(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: true,
		emailType:   "NONE",
		hosts: []fakeHost{
			{id: 1, name: "www", rtype: "A", address: "192.0.2.1", mxPref: 10, ttl: 1800},
			{id: 2, name: "blog", rtype: "A", address: "203.0.113.9", mxPref: 10, ttl: 1800},
		},
	})

	r := &DomainRecords{Domain: itDomain, Mode: "MERGE"}
	prior := &DomainRecordsOutput{
		Domain:  itDomain,
		Mode:    "MERGE",
		Records: []RecordOutput{{Hostname: "www", Type: "A", Address: "192.0.2.1"}},
	}
	require.NoError(t, r.Delete(context.Background(), f.configuration(), prior))

	// Only the managed www record is removed; the unmanaged blog record stays.
	assert.Equal(t, []string{recordKey("blog", "A", "203.0.113.9")},
		hostKeys(f.state(itDomain).hosts))
}

func TestRelativeHost(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		domain   string
		want     string
	}{
		{name: "fqdn under domain", hostname: "_abc.www.example.com", domain: "example.com", want: "_abc.www"},
		{name: "fqdn with trailing dot", hostname: "_abc.www.example.com.", domain: "example.com", want: "_abc.www"},
		{name: "bare domain is apex", hostname: "example.com", domain: "example.com", want: "@"},
		{name: "bare domain trailing dot is apex", hostname: "example.com.", domain: "example.com", want: "@"},
		{name: "relative label unchanged", hostname: "www", domain: "example.com", want: "www"},
		{name: "apex unchanged", hostname: "@", domain: "example.com", want: "@"},
		{name: "relative multi-label unchanged", hostname: "_abc.www", domain: "example.com", want: "_abc.www"},
		{name: "case is normalized", hostname: "WWW.Example.com", domain: "example.com", want: "www"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, relativeHost(tt.hostname, tt.domain))
		})
	}
}

func TestDomainRecordsCreateMergeRelativizesFQDNHost(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{usingOurDNS: true, emailType: "NONE"})

	// ACM reports a validation record's name as a FQDN under the domain. The
	// resource must send Namecheap the relative label, or Namecheap appends the
	// zone and doubles it.
	r := &DomainRecords{
		Domain: itDomain,
		Mode:   "MERGE",
		Records: []Record{
			{Hostname: "_abc123.www.example.com", Type: "CNAME", Address: "_x.acm-validations.aws."},
		},
	}
	out, err := r.Create(context.Background(), f.configuration())
	require.NoError(t, err)

	// The write sends the relative host, not the FQDN.
	assert.ElementsMatch(t, []sentRecord{
		{Hostname: "_abc123.www", Type: "CNAME", Address: "_x.acm-validations.aws.", MXPref: "10", TTL: "1800"},
	}, recordsFromForm(lastForm(t, f, "namecheap.domains.dns.setHosts")))

	// The record pairs with its remote twin, so the read reports it present
	// while the output echoes the hostname as configured.
	assert.Equal(t, []string{
		recordKey("_abc123.www.example.com", "CNAME", "_x.acm-validations.aws."),
	}, outputKeys(out.Records))
}
