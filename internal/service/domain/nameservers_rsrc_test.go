package domain

import (
	"context"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainNameserversCreateOverwrite(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{usingOurDNS: true})

	r := &DomainNameservers{
		Domain:      itDomain,
		Mode:        "OVERWRITE",
		Nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	}
	out, err := r.Create(context.Background(), f.configuration())
	require.NoError(t, err)

	state := f.state(itDomain)
	assert.False(t, state.usingOurDNS)
	assert.ElementsMatch(t, []string{"a.ns.example.net", "b.ns.example.net"}, state.nameservers)
	assert.ElementsMatch(t, []string{"a.ns.example.net", "b.ns.example.net"}, out.Nameservers)
}

func TestDomainNameserversCreateMergeOnDefaultDNS(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{usingOurDNS: true})

	r := &DomainNameservers{
		Domain:      itDomain,
		Mode:        "MERGE",
		Nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	}
	_, err := r.Create(context.Background(), f.configuration())
	require.NoError(t, err)

	// On a domain still using Namecheap DNS, a merge delegates to exactly the
	// configured nameservers.
	assert.ElementsMatch(t, []string{"a.ns.example.net", "b.ns.example.net"},
		f.state(itDomain).nameservers)
}

func TestDomainNameserversCreateMergeWithExisting(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: false,
		nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	})

	r := &DomainNameservers{
		Domain:      itDomain,
		Mode:        "MERGE",
		Nameservers: []string{"c.ns.example.net", "d.ns.example.net"},
	}
	_, err := r.Create(context.Background(), f.configuration())
	require.NoError(t, err)

	// The merge appends its nameservers to the ones already delegated.
	assert.ElementsMatch(t, []string{
		"a.ns.example.net", "b.ns.example.net",
		"c.ns.example.net", "d.ns.example.net",
	}, f.state(itDomain).nameservers)
}

func TestDomainNameserversCreateMergeDuplicate(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: false,
		nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	})

	r := &DomainNameservers{
		Domain:      itDomain,
		Mode:        "MERGE",
		Nameservers: []string{"A.NS.EXAMPLE.NET", "c.ns.example.net"},
	}
	_, err := r.Create(context.Background(), f.configuration())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate nameserver")
}

func TestDomainNameserversReadOverwriteReturnsAll(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: false,
		nameservers: []string{"a.ns.example.net", "b.ns.example.net", "manual.ns.example.net"},
	})

	r := &DomainNameservers{
		Domain:      itDomain,
		Mode:        "OVERWRITE",
		Nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	}
	out, err := r.Read(context.Background(), f.configuration(), nil)
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"a.ns.example.net", "b.ns.example.net", "manual.ns.example.net"},
		out.Nameservers)
}

func TestDomainNameserversReadMergeReturnsManaged(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: false,
		nameservers: []string{"a.ns.example.net", "b.ns.example.net", "manual.ns.example.net"},
	})

	r := &DomainNameservers{
		Domain:      itDomain,
		Mode:        "MERGE",
		Nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	}
	out, err := r.Read(context.Background(), f.configuration(), nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a.ns.example.net", "b.ns.example.net"}, out.Nameservers)
}

func TestDomainNameserversReadDefaultDNSNotFound(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{usingOurDNS: true})

	r := &DomainNameservers{
		Domain:      itDomain,
		Mode:        "MERGE",
		Nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	}
	_, err := r.Read(context.Background(), f.configuration(), nil)
	assert.ErrorIs(t, err, runtime.ErrNotFound)
}

func TestDomainNameserversUpdateMerge(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: false,
		nameservers: []string{"a.ns.example.net", "b.ns.example.net", "manual.ns.example.net"},
	})

	r := &DomainNameservers{
		Domain:      itDomain,
		Mode:        "MERGE",
		Nameservers: []string{"c.ns.example.net", "d.ns.example.net"},
	}
	prior := runtime.Prior[DomainNameservers, *DomainNameserversOutput]{
		Inputs: DomainNameservers{
			Domain:      itDomain,
			Mode:        "MERGE",
			Nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
		},
	}
	_, err := r.Update(context.Background(), f.configuration(), prior)
	require.NoError(t, err)

	// The prior nameservers give way to the new ones; the manually added one
	// stays.
	assert.ElementsMatch(t, []string{
		"manual.ns.example.net", "c.ns.example.net", "d.ns.example.net",
	}, f.state(itDomain).nameservers)
}

func TestDomainNameserversUpdateOverwrite(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: false,
		nameservers: []string{"old1.ns.example.net", "old2.ns.example.net"},
	})

	r := &DomainNameservers{
		Domain:      itDomain,
		Mode:        "OVERWRITE",
		Nameservers: []string{"new1.ns.example.net", "new2.ns.example.net"},
	}
	prior := runtime.Prior[DomainNameservers, *DomainNameserversOutput]{
		Inputs: DomainNameservers{Domain: itDomain, Mode: "OVERWRITE"},
	}
	_, err := r.Update(context.Background(), f.configuration(), prior)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"new1.ns.example.net", "new2.ns.example.net"},
		f.state(itDomain).nameservers)
}

func TestDomainNameserversDeleteOverwrite(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: false,
		nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	})

	r := &DomainNameservers{Domain: itDomain, Mode: "OVERWRITE"}
	prior := &DomainNameserversOutput{
		Domain:      itDomain,
		Mode:        "OVERWRITE",
		Nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	}
	require.NoError(t, r.Delete(context.Background(), f.configuration(), prior))

	assert.NotEmpty(t, f.sent("namecheap.domains.dns.setDefault"))
	assert.True(t, f.state(itDomain).usingOurDNS)
}

func TestDomainNameserversDeleteMergeResetsToDefault(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: false,
		nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	})

	r := &DomainNameservers{Domain: itDomain, Mode: "MERGE"}
	prior := &DomainNameserversOutput{
		Domain:      itDomain,
		Mode:        "MERGE",
		Nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	}
	require.NoError(t, r.Delete(context.Background(), f.configuration(), prior))

	// Removing the last managed nameservers returns the domain to default DNS.
	assert.NotEmpty(t, f.sent("namecheap.domains.dns.setDefault"))
	assert.True(t, f.state(itDomain).usingOurDNS)
}

func TestDomainNameserversDeleteMergePreservesOthers(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: false,
		nameservers: []string{
			"a.ns.example.net", "b.ns.example.net",
			"manual1.ns.example.net", "manual2.ns.example.net",
		},
	})

	r := &DomainNameservers{Domain: itDomain, Mode: "MERGE"}
	prior := &DomainNameserversOutput{
		Domain:      itDomain,
		Mode:        "MERGE",
		Nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	}
	require.NoError(t, r.Delete(context.Background(), f.configuration(), prior))

	// Only the managed nameservers are removed; the manual ones remain delegated.
	assert.ElementsMatch(t, []string{"manual1.ns.example.net", "manual2.ns.example.net"},
		f.state(itDomain).nameservers)
}

func TestDomainNameserversDeleteMergeOneRemainingErrors(t *testing.T) {
	f := newFakeNamecheap(t)
	f.seed(itDomain, fakeDomain{
		usingOurDNS: false,
		nameservers: []string{"a.ns.example.net", "b.ns.example.net", "manual.ns.example.net"},
	})

	r := &DomainNameservers{Domain: itDomain, Mode: "MERGE"}
	prior := &DomainNameserversOutput{
		Domain:      itDomain,
		Mode:        "MERGE",
		Nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
	}
	err := r.Delete(context.Background(), f.configuration(), prior)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 2 nameservers")
}
