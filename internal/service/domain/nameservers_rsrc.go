package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// DomainNameservers delegates a Namecheap domain to a set of custom
// nameservers, which turns off Namecheap's own DNS for the domain (and so
// cannot coexist with domain-records). The mode decides how it shares the
// delegation with nameservers it does not define. In overwrite mode it owns the
// whole set -- a write replaces every nameserver and a delete returns the domain
// to Namecheap's default DNS. In merge mode it owns only the nameservers it
// lists -- a write preserves any added elsewhere, and a delete removes only its
// own, returning to default DNS once none remain. The domain identifies the
// resource; changing it replaces the resource.
type DomainNameservers struct {
	Domain      string   `ub:"domain"`
	Mode        string   `ub:"mode"`
	Nameservers []string `ub:"nameservers"`
}

// DomainNameserversOutput reports the domain, the resolved mode, and the
// nameservers the resource owns. The nameservers carry the identity a
// merge-mode Delete needs, since the runtime hands Delete the prior output
// rather than the prior inputs.
type DomainNameserversOutput struct {
	Domain      string   `ub:"domain"`
	Mode        string   `ub:"mode"`
	Nameservers []string `ub:"nameservers"`
}

func (r *DomainNameservers) SchemaVersion() int { return 1 }

// ReplaceFields lists the input that identifies the resource. The domain is the
// delegation's home; a different domain is a different delegation. Mode and the
// nameservers reconcile in place.
func (r *DomainNameservers) ReplaceFields() []string {
	return []string{"domain"}
}

// Defaults marks mode as defaulting to merge. The nameservers are required.
func (r DomainNameservers) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.Mode, "MERGE"),
	}
}

// Constraints declares the rules Namecheap places on the inputs. The domain is
// required, the mode is merge or overwrite, and at least two nameservers are
// given, the minimum Namecheap accepts for a custom delegation.
func (r DomainNameservers) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.Domain)),
		constraint.Must(constraint.OneOf(r.Mode, "MERGE", "OVERWRITE")),
		constraint.Must(constraint.MinItems(r.Nameservers, 2)).
			Message("a domain must have at least 2 nameservers"),
	}
}

func (r *DomainNameservers) Create(ctx context.Context, cfg any) (*DomainNameserversOutput, error) {
	client, err := newClient(cfg)
	if err != nil {
		return nil, err
	}
	domain := strings.ToLower(r.Domain)
	if r.mergeMode() {
		err = createMergeNameservers(client, domain, r.Nameservers)
	} else {
		err = setCustom(client, domain, r.Nameservers)
	}
	if err != nil {
		return nil, err
	}
	return r.read(client)
}

func (r *DomainNameservers) Read(
	ctx context.Context, cfg any, prior *DomainNameserversOutput,
) (*DomainNameserversOutput, error) {
	client, err := newClient(cfg)
	if err != nil {
		return nil, err
	}
	return r.read(client)
}

func (r *DomainNameservers) Update(
	ctx context.Context, cfg any, prior runtime.Prior[DomainNameservers, *DomainNameserversOutput],
) (*DomainNameserversOutput, error) {
	client, err := newClient(cfg)
	if err != nil {
		return nil, err
	}
	domain := strings.ToLower(r.Domain)
	if r.mergeMode() {
		err = updateMergeNameservers(client, domain, prior.Inputs.Nameservers, r.Nameservers)
	} else {
		err = setCustom(client, domain, r.Nameservers)
	}
	if err != nil {
		return nil, err
	}
	return r.read(client)
}

func (r *DomainNameservers) Delete(
	ctx context.Context, cfg any, prior *DomainNameserversOutput,
) error {
	client, err := newClient(cfg)
	if err != nil {
		return err
	}
	domain := strings.ToLower(r.Domain)
	mode := modeMerge
	var managed []string
	if prior != nil {
		if prior.Domain != "" {
			domain = strings.ToLower(prior.Domain)
		}
		if prior.Mode != "" {
			mode = strings.ToUpper(prior.Mode)
		}
		managed = prior.Nameservers
	}
	if mode == modeOverwrite {
		return setDefault(client, domain)
	}
	return deleteMergeNameservers(client, domain, managed)
}

// read returns the managed nameservers. A domain on Namecheap's default DNS has
// no custom nameservers for this resource to own, so it reads as
// runtime.ErrNotFound and a plan recreates it. Merge mode reports the
// configured nameservers that are present; overwrite mode reports them all.
func (r *DomainNameservers) read(client *namecheap.Client) (*DomainNameserversOutput, error) {
	domain := strings.ToLower(r.Domain)
	resp, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return nil, err
	}
	ourDNS, err := usingOurDNS(resp)
	if err != nil {
		return nil, err
	}
	if ourDNS {
		return nil, runtime.ErrNotFound
	}
	remote := nameserversOf(resp)
	var managed []string
	if r.mergeMode() {
		for _, ns := range r.Nameservers {
			if containsFold(remote, ns) {
				managed = append(managed, ns)
			}
		}
	} else {
		managed = append(managed, remote...)
	}
	return &DomainNameserversOutput{
		Domain:      r.Domain,
		Mode:        r.normalizedMode(),
		Nameservers: managed,
	}, nil
}

func (r *DomainNameservers) mergeMode() bool {
	return r.normalizedMode() == modeMerge
}

func (r *DomainNameservers) normalizedMode() string {
	return strings.ToUpper(r.Mode)
}

// createMergeNameservers sets the desired nameservers, merging with any already
// delegated. A desired nameserver already present is a duplicate and fails the
// apply. A domain still on Namecheap's DNS is delegated to exactly the desired
// nameservers.
func createMergeNameservers(client *namecheap.Client, domain string, desired []string) error {
	resp, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return fmt.Errorf("get dns list: %w", err)
	}
	ourDNS, err := usingOurDNS(resp)
	if err != nil {
		return err
	}
	if ourDNS {
		return setCustom(client, domain, desired)
	}
	remote := nameserversOf(resp)
	merged := append([]string{}, remote...)
	for _, ns := range desired {
		if containsFold(remote, ns) {
			return fmt.Errorf("duplicate nameserver %s already exists", ns)
		}
		merged = append(merged, ns)
	}
	return setCustom(client, domain, merged)
}

// updateMergeNameservers rebuilds the custom delegation so the nameservers
// owned before give way to the ones owned now, preserving any added elsewhere.
// Removing every nameserver returns the domain to default DNS; leaving exactly
// one is rejected, since Namecheap requires at least two.
func updateMergeNameservers(
	client *namecheap.Client, domain string, prior, current []string,
) error {
	resp, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return fmt.Errorf("get dns list: %w", err)
	}
	ourDNS, err := usingOurDNS(resp)
	if err != nil {
		return err
	}
	var kept []string
	if !ourDNS {
		for _, ns := range nameserversOf(resp) {
			if !containsFold(prior, ns) {
				kept = append(kept, ns)
			}
		}
	}
	kept = append(kept, current...)
	return applyNameserverSet(client, domain, kept)
}

// deleteMergeNameservers removes the managed nameservers from the live
// delegation, leaving any added elsewhere. A domain already on default DNS has
// nothing to remove.
func deleteMergeNameservers(client *namecheap.Client, domain string, managed []string) error {
	resp, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return fmt.Errorf("get dns list: %w", err)
	}
	ourDNS, err := usingOurDNS(resp)
	if err != nil {
		return err
	}
	if ourDNS {
		return nil
	}
	var remaining []string
	for _, ns := range nameserversOf(resp) {
		if !containsFold(managed, ns) {
			remaining = append(remaining, ns)
		}
	}
	return applyNameserverSet(client, domain, remaining)
}

// applyNameserverSet writes a computed nameserver set, returning the domain to
// default DNS when none remain and rejecting a lone nameserver, which Namecheap
// does not allow.
func applyNameserverSet(client *namecheap.Client, domain string, nameservers []string) error {
	switch len(nameservers) {
	case 1:
		return fmt.Errorf(
			"a domain must have at least 2 nameservers, but only 1 would remain")
	case 0:
		return setDefault(client, domain)
	default:
		return setCustom(client, domain, nameservers)
	}
}
