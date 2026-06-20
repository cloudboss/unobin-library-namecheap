package domain

import (
	"context"
	"strings"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-namecheap/internal/config"
	"github.com/cloudboss/unobin-library-namecheap/internal/ptr"
)

// DomainRecords manages the host DNS records of a Namecheap domain, together
// with the domain's email type. Namecheap has no per-record API: every write is
// a full SetHosts of the domain's record set and every read a full GetHosts, so
// this resource owns a set of records rather than a single one. The mode decides
// how it shares the domain with records it does not define. In overwrite mode it
// owns the whole record set -- a write replaces every record, and a read reports
// them all, so a record added elsewhere is drift the resource removes. In merge
// mode it owns only the records it lists -- a write reads the live set, swaps its
// own records, and writes the union back, leaving every other record in place.
// The domain identifies the resource; changing it replaces the resource.
type DomainRecords struct {
	Domain    string   `ub:"domain"`
	Mode      string   `ub:"mode"`
	EmailType *string  `ub:"email-type"`
	Records   []Record `ub:"records"`
}

// DomainRecordsOutput reports the domain, the resolved mode and email type, and
// the records the resource owns. The records carry the identity a merge-mode
// Delete needs, since the runtime hands Delete the prior output rather than the
// prior inputs.
type DomainRecordsOutput struct {
	Domain    string         `ub:"domain"`
	Mode      string         `ub:"mode"`
	EmailType string         `ub:"email-type"`
	Records   []RecordOutput `ub:"records"`
}

func (r *DomainRecords) SchemaVersion() int { return 1 }

// ReplaceFields lists the input that identifies the resource. The domain is the
// record set's home; a different domain is a different record set, so unobin's
// replace removes the old one and creates the new. Mode, email type, and the
// records reconcile in place.
func (r *DomainRecords) ReplaceFields() []string {
	return []string{"domain"}
}

// Defaults marks mode as defaulting to merge and the record list as omittable;
// a resource with no records still manages the domain's email type, and in
// overwrite mode clears the record set.
func (r DomainRecords) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.Mode, "MERGE"),
		defaults.Optional(r.Records),
	}
}

// Constraints declares the rules Namecheap places on the inputs. The domain is
// required, the mode is merge or overwrite, and the email type is one of the
// accepted values. Each record's type is a known Namecheap record type, its TTL
// is within the accepted range, and its MX preference fits a byte.
func (r DomainRecords) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.Domain)),
		constraint.Must(constraint.OneOf(r.Mode, "MERGE", "OVERWRITE")),
		constraint.When(constraint.Present(r.EmailType)).
			Require(constraint.OneOf(r.EmailType,
				"NONE", "MXE", "MX", "FWD", "OX", "GMAIL")).
			Message("email-type must be one of NONE, MXE, MX, FWD, OX, or GMAIL"),
		constraint.ForEach(r.Records, func(rec Record) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(rec.Type,
					"A", "AAAA", "ALIAS", "CAA", "CNAME", "MX",
					"MXE", "NS", "TXT", "URL", "URL301", "FRAME")).
					Message("a record type must be a valid Namecheap record type"),
				constraint.When(constraint.Present(rec.TTL)).
					Require(constraint.AtLeast(rec.TTL, 60), constraint.AtMost(rec.TTL, 60000)).
					Message("a record ttl must be between 60 and 60000"),
				constraint.When(constraint.Present(rec.MXPref)).
					Require(constraint.AtLeast(rec.MXPref, 0), constraint.AtMost(rec.MXPref, 255)).
					Message("a record mx-pref must be between 0 and 255"),
			}
		}),
	}
}

func (r *DomainRecords) Create(
	ctx context.Context,
	cfg *config.Configuration,
) (*DomainRecordsOutput, error) {
	client := newClient(cfg)
	domain := strings.ToLower(r.Domain)
	// A domain on custom nameservers rejects a host-record write, so return it
	// to Namecheap's DNS first. On the normal path it already is, and this is a
	// single read.
	if err := ensureOurDNS(client, domain); err != nil {
		return nil, err
	}
	desired := r.desiredHostRecords()
	var err error
	if r.mergeMode() {
		err = createMergeHosts(client, domain, desired, r.EmailType)
	} else {
		err = overwriteHosts(client, domain, desired, r.EmailType)
	}
	if err != nil {
		return nil, err
	}
	return r.read(client, nil)
}

func (r *DomainRecords) Read(
	ctx context.Context,
	cfg *config.Configuration,
	prior *DomainRecordsOutput,
) (*DomainRecordsOutput, error) {
	client := newClient(cfg)
	return r.read(client, prior)
}

func (r *DomainRecords) Update(
	ctx context.Context,
	cfg *config.Configuration,
	prior runtime.Prior[DomainRecords, *DomainRecordsOutput],
) (*DomainRecordsOutput, error) {
	client := newClient(cfg)
	domain := strings.ToLower(r.Domain)
	if err := ensureOurDNS(client, domain); err != nil {
		return nil, err
	}
	desired := r.desiredHostRecords()
	if r.mergeMode() {
		priorHashes, err := hashSetOfInputs(prior.Inputs.Records, r.Domain)
		if err != nil {
			return nil, err
		}
		err = updateMergeHosts(client, domain, priorHashes, desired, r.EmailType)
		if err != nil {
			return nil, err
		}
	} else {
		if err := overwriteHosts(client, domain, desired, r.EmailType); err != nil {
			return nil, err
		}
	}
	return r.read(client, nil)
}

func (r *DomainRecords) Delete(
	ctx context.Context,
	cfg *config.Configuration,
	prior *DomainRecordsOutput,
) error {
	client := newClient(cfg)
	// A replace decodes the new inputs into the receiver before Delete runs, so
	// the record set to remove is named by the prior output -- its domain, mode,
	// and the records the prior apply owned.
	domain := strings.ToLower(r.Domain)
	mode := modeMerge
	var managed []RecordOutput
	if prior != nil {
		if prior.Domain != "" {
			domain = strings.ToLower(prior.Domain)
		}
		if prior.Mode != "" {
			mode = strings.ToUpper(prior.Mode)
		}
		managed = prior.Records
	}
	if mode == modeOverwrite {
		// Overwrite owns the whole set, so deleting it clears every record.
		return setHosts(client, domain, nil, namecheap.String(namecheap.EmailTypeNone))
	}
	priorHashes, err := recordHashSet(managed, domain)
	if err != nil {
		return err
	}
	return deleteMergeHosts(client, domain, priorHashes)
}

// read returns the resource's settled output. A domain delegated to custom
// nameservers has no host records this resource could own, so it reads as
// runtime.ErrNotFound and a plan recreates it, which returns the domain to
// Namecheap's DNS. Merge mode checks the prior output when present, or the
// configured records otherwise. Overwrite mode keeps every live record.
func (r *DomainRecords) read(
	client *namecheap.Client,
	prior *DomainRecordsOutput,
) (*DomainRecordsOutput, error) {
	domain := strings.ToLower(r.Domain)
	listResp, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return nil, err
	}
	ourDNS, err := usingOurDNS(listResp)
	if err != nil {
		return nil, err
	}
	if !ourDNS {
		return nil, runtime.ErrNotFound
	}
	hostsResp, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return nil, err
	}
	remote := hostsOf(hostsResp)
	var records []RecordOutput
	if r.mergeMode() {
		records = r.matchedRecords(remote, prior)
	} else {
		records = r.allRecords(remote)
	}
	return &DomainRecordsOutput{
		Domain:    r.Domain,
		Mode:      r.normalizedMode(),
		EmailType: ptr.Deref(emailTypeOf(hostsResp)),
		Records:   records,
	}, nil
}

// matchedRecords returns the records named by recordsForRead that are present
// remotely, preserving their order and stored address text.
func (r *DomainRecords) matchedRecords(
	remote []namecheap.DomainsDNSHostRecordDetailed,
	prior *DomainRecordsOutput,
) []RecordOutput {
	present := make(map[string]bool, len(remote))
	for _, d := range remote {
		present[detailedHash(d)] = true
	}
	var out []RecordOutput
	for _, rec := range r.recordsForRead(prior) {
		h, err := managedHash(relativeHost(rec.Hostname, r.Domain), rec.Type, rec.Address)
		if err != nil {
			continue
		}
		if present[h] {
			out = append(out, rec)
		}
	}
	return out
}

func (r *DomainRecords) recordsForRead(prior *DomainRecordsOutput) []RecordOutput {
	if prior != nil {
		return prior.Records
	}
	records := make([]RecordOutput, 0, len(r.Records))
	for _, rec := range r.Records {
		records = append(records, RecordOutput{
			Hostname: rec.Hostname,
			Type:     rec.Type,
			Address:  rec.Address,
		})
	}
	return records
}

// allRecords returns every live record in API order, presenting a record that
// matches a configured one with the address as configured so the round trip
// hides Namecheap's canonicalization.
func (r *DomainRecords) allRecords(
	remote []namecheap.DomainsDNSHostRecordDetailed,
) []RecordOutput {
	configured := map[string]string{}
	for _, rec := range r.Records {
		h, err := managedHash(relativeHost(rec.Hostname, r.Domain), rec.Type, rec.Address)
		if err != nil {
			continue
		}
		configured[h] = rec.Address
	}
	var out []RecordOutput
	for _, d := range remote {
		address := ptr.Deref(d.Address)
		if configuredAddr, ok := configured[detailedHash(d)]; ok {
			address = configuredAddr
		}
		out = append(out, RecordOutput{
			Hostname: ptr.Deref(d.Name),
			Type:     ptr.Deref(d.Type),
			Address:  address,
		})
	}
	return out
}

// desiredHostRecords converts the configured records into the SDK form a write
// takes.
func (r *DomainRecords) desiredHostRecords() []namecheap.DomainsDNSHostRecord {
	records := make([]namecheap.DomainsDNSHostRecord, 0, len(r.Records))
	for _, rec := range r.Records {
		rec.Hostname = relativeHost(rec.Hostname, r.Domain)
		records = append(records, rec.hostRecord())
	}
	return records
}

func (r *DomainRecords) mergeMode() bool {
	return r.normalizedMode() == modeMerge
}

func (r *DomainRecords) normalizedMode() string {
	return strings.ToUpper(r.Mode)
}

// hashSetOfInputs builds the identity-hash set for a list of configured
// records, used to find the records a prior apply owned.
func hashSetOfInputs(records []Record, domain string) (map[string]bool, error) {
	set := make(map[string]bool, len(records))
	for _, rec := range records {
		h, err := managedHash(relativeHost(rec.Hostname, domain), rec.Type, rec.Address)
		if err != nil {
			return nil, err
		}
		set[h] = true
	}
	return set, nil
}

// createMergeHosts reads the live records, unions the desired records in, and
// writes the result. The email type, when not set, is resolved from the merged
// records and the live email type.
func createMergeHosts(
	client *namecheap.Client,
	domain string,
	desired []namecheap.DomainsDNSHostRecord,
	emailType *string,
) error {
	resp, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return err
	}
	merged, err := mergeCreateRecords(hostsOf(resp), desired, domain)
	if err != nil {
		return err
	}
	if emailType == nil {
		emailType = resolveEmailType(merged, emailTypeOf(resp))
	}
	return setHosts(client, domain, merged, emailType)
}

// updateMergeHosts rewrites the record set so the records named by priorHashes
// give way to the desired records, leaving every other live record in place.
func updateMergeHosts(
	client *namecheap.Client,
	domain string,
	priorHashes map[string]bool,
	desired []namecheap.DomainsDNSHostRecord,
	emailType *string,
) error {
	resp, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return err
	}
	merged := mergeUpdateRecords(hostsOf(resp), priorHashes, desired)
	if emailType == nil {
		emailType = resolveEmailType(merged, emailTypeOf(resp))
	}
	return setHosts(client, domain, merged, emailType)
}

// deleteMergeHosts removes the records named by priorHashes from the live set
// and writes the remainder, resolving the email type against what is left.
func deleteMergeHosts(client *namecheap.Client, domain string, priorHashes map[string]bool) error {
	resp, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return err
	}
	remaining := mergeUpdateRecords(hostsOf(resp), priorHashes, nil)
	emailType := resolveEmailType(remaining, emailTypeOf(resp))
	return setHosts(client, domain, remaining, emailType)
}

// overwriteHosts replaces the whole record set with the desired records. An
// unset email type becomes NONE.
func overwriteHosts(
	client *namecheap.Client,
	domain string,
	desired []namecheap.DomainsDNSHostRecord,
	emailType *string,
) error {
	if emailType == nil {
		emailType = namecheap.String(namecheap.EmailTypeNone)
	}
	sortHostRecords(desired)
	return setHosts(client, domain, desired, emailType)
}
