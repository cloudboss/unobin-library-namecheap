package domain

import (
	"context"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-namecheap/internal/config"
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

func TestDomainNameserversDefinition(t *testing.T) {
	fake := newFakeNamecheap(t)
	definition := DomainNameserversDefinition()
	require.Equal(t, 1, definition.SchemaVersion)
	require.NotNil(t, definition.Validate)
	require.NotPanics(t, func() {
		runtime.MakeResource[
			DomainNameservers,
			*DomainNameserversOutput,
			*config.Configuration,
		](definition)
	})
	require.NoError(t, definition.Validate(context.Background(), DomainNameservers{
		Domain:      itDomain,
		Mode:        "MERGE",
		Nameservers: []string{"ns1.example.net", "ns2.example.net"},
	}, fake.configuration()))
	require.Empty(t, fake.sent("namecheap.domains.dns.setCustom"))
	require.Empty(t, fake.sent("namecheap.domains.dns.setDefault"))
}

func TestDomainNameserversDefinitionRejectsDuplicateNameservers(t *testing.T) {
	definition := DomainNameserversDefinition()
	err := definition.Validate(context.Background(), DomainNameservers{
		Domain:      itDomain,
		Mode:        "MERGE",
		Nameservers: []string{"ns1.example.net", "NS1.EXAMPLE.NET"},
	}, &config.Configuration{})
	require.ErrorContains(t, err, "duplicate nameserver")
}

func TestDomainNameserversDeleteUsesPriorTargetAndCurrentCredentials(t *testing.T) {
	priorAPI := newFakeNamecheap(t)
	priorAPI.seed(itDomain, fakeDomain{
		usingOurDNS: false,
		nameservers: []string{"ns1.example.net", "ns2.example.net"},
	})
	desiredAPI := newFakeNamecheap(t)

	priorInput := DomainNameservers{Domain: itDomain, Mode: "OVERWRITE"}
	prior := domainNameserversPrior(priorInput, &DomainNameserversOutput{
		Domain:      itDomain,
		Mode:        "OVERWRITE",
		Nameservers: []string{"ns1.example.net", "ns2.example.net"},
	})
	prior.Configuration = priorAPI.configurationWithKey("revoked-key")
	desired := &DomainNameservers{Domain: "new.example.com", Mode: "OVERWRITE"}

	err := desired.Delete(
		context.Background(),
		desiredAPI.configurationWithKey("current-key"),
		prior,
	)
	require.NoError(t, err)
	require.True(t, priorAPI.state(itDomain).usingOurDNS)
	require.Empty(t, desiredAPI.sent("namecheap.domains.dns.setDefault"))
	form := lastForm(t, priorAPI, "namecheap.domains.dns.setDefault")
	require.Equal(t, "current-key", form.Get("ApiKey"))
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
	out, err := r.Read(
		context.Background(),
		f.configuration(),
		domainNameserversPrior(*r, nil),
	)
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
	out, err := r.Read(
		context.Background(),
		f.configuration(),
		domainNameserversPrior(*r, nil),
	)
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
	_, err := r.Read(
		context.Background(),
		f.configuration(),
		domainNameserversPrior(*r, nil),
	)
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
	prior := domainNameserversPrior(
		DomainNameservers{
			Domain:      itDomain,
			Mode:        "MERGE",
			Nameservers: []string{"a.ns.example.net", "b.ns.example.net"},
		},
		nil,
	)
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
	prior := domainNameserversPrior(
		DomainNameservers{Domain: itDomain, Mode: "OVERWRITE"},
		nil,
	)
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
	require.NoError(t, r.Delete(
		context.Background(),
		f.configuration(),
		domainNameserversPrior(*r, prior),
	))

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
	require.NoError(t, r.Delete(
		context.Background(),
		f.configuration(),
		domainNameserversPrior(*r, prior),
	))

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
	require.NoError(t, r.Delete(
		context.Background(),
		f.configuration(),
		domainNameserversPrior(*r, prior),
	))

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
	err := r.Delete(
		context.Background(),
		f.configuration(),
		domainNameserversPrior(*r, prior),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 2 nameservers")
}
