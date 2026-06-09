package domain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"

	"github.com/cloudboss/unobin-library-namecheap/internal/config"
	"github.com/cloudboss/unobin-library-namecheap/internal/ptr"
)

// mode values decide how a resource shares a domain with records or
// nameservers it does not define. Overwrite owns the whole set; merge owns only
// what it lists, leaving everything else in place.
const (
	modeMerge     = "MERGE"
	modeOverwrite = "OVERWRITE"
)

// newClient unwraps the configuration the runtime hands every lifecycle method
// and builds a Namecheap API client from it. The go-namecheap-sdk is not
// context-aware, so ctx does not reach the API calls; the lifecycle methods
// still carry it to satisfy the runtime contract.
func newClient(cfg any) (*namecheap.Client, error) {
	c, ok := cfg.(*config.Configuration)
	if !ok {
		return nil, fmt.Errorf("namecheapclient: unexpected configuration type %T", cfg)
	}
	return config.NewClient(c), nil
}

// hashRecord identifies a host record by hostname, type, and address. The
// hostname is lowercased and the type uppercased so two spellings of one record
// compare equal; the address is left as is, since its case is significant for
// types such as TXT.
func hashRecord(hostname, recordType, address string) string {
	return fmt.Sprintf("[%s:%s:%s]",
		strings.ToLower(hostname), strings.ToUpper(recordType), address)
}

// fixedAddress returns the address in the canonical form Namecheap stores it,
// used only to pair a desired record with its remote twin when hashing. CNAME,
// ALIAS, NS, and MX records gain a trailing dot; a CAA record's value is wrapped
// in quotes. Every other type is returned unchanged.
func fixedAddress(recordType, address string) (string, error) {
	switch recordType {
	case namecheap.RecordTypeCNAME, namecheap.RecordTypeAlias,
		namecheap.RecordTypeNS, namecheap.RecordTypeMX:
		if !strings.HasSuffix(address, ".") {
			return address + ".", nil
		}
		return address, nil
	case namecheap.RecordTypeCAA:
		return fixCAAAddress(address)
	default:
		return address, nil
	}
}

// fixCAAAddress wraps the value field of a CAA address in quotes when it has
// none, the form Namecheap stores. A CAA address is three space-separated
// fields (flags, tag, value); anything else, or a half-quoted value, is an
// error.
func fixCAAAddress(address string) (string, error) {
	parts := strings.Fields(address)
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid CAA address %q: want three fields", address)
	}
	hasPrefix := strings.HasPrefix(parts[2], `"`)
	hasSuffix := strings.HasSuffix(parts[2], `"`)
	switch {
	case !hasPrefix && !hasSuffix:
		parts[2] = `"` + parts[2] + `"`
	case hasPrefix != hasSuffix:
		return "", fmt.Errorf("invalid CAA address %q: mismatched quotes", address)
	}
	return strings.Join(parts, " "), nil
}

// desiredHash hashes a configured host record after canonicalizing its address,
// so it matches the hash of the same record read back from Namecheap.
func desiredHash(rec namecheap.DomainsDNSHostRecord) (string, error) {
	fixed, err := fixedAddress(ptr.Deref(rec.RecordType), ptr.Deref(rec.Address))
	if err != nil {
		return "", err
	}
	return hashRecord(ptr.Deref(rec.HostName), ptr.Deref(rec.RecordType), fixed), nil
}

// detailedHash hashes a host record read from Namecheap, whose address is
// already in canonical form.
func detailedHash(d namecheap.DomainsDNSHostRecordDetailed) string {
	return hashRecord(ptr.Deref(d.Name), ptr.Deref(d.Type), ptr.Deref(d.Address))
}

// managedHash hashes the identity of a record this resource manages, given its
// hostname, type, and configured address.
func managedHash(hostname, recordType, address string) (string, error) {
	fixed, err := fixedAddress(recordType, address)
	if err != nil {
		return "", err
	}
	return hashRecord(hostname, recordType, fixed), nil
}

// recordHashSet builds the set of identity hashes for a list of managed
// records.
func recordHashSet(records []RecordOutput) (map[string]bool, error) {
	set := make(map[string]bool, len(records))
	for _, rec := range records {
		h, err := managedHash(rec.Hostname, rec.Type, rec.Address)
		if err != nil {
			return nil, err
		}
		set[h] = true
	}
	return set, nil
}

// hostRecordFromDetailed converts a host record read from Namecheap into the
// form a SetHosts write takes, so an unmanaged record can be carried through a
// merge untouched.
func hostRecordFromDetailed(
	d namecheap.DomainsDNSHostRecordDetailed,
) namecheap.DomainsDNSHostRecord {
	return namecheap.DomainsDNSHostRecord{
		HostName:   d.Name,
		RecordType: d.Type,
		Address:    d.Address,
		MXPref:     new(uint8(ptr.Deref(d.MXPref))),
		TTL:        d.TTL,
	}
}

// filterParkingRecords drops the default records Namecheap parks on a fresh
// domain: the www CNAME to its parking page and the apex URL forward to the
// www site. Carrying them through a merge would re-create them as managed
// records.
func filterParkingRecords(
	records []namecheap.DomainsDNSHostRecordDetailed, domain string,
) []namecheap.DomainsDNSHostRecordDetailed {
	var out []namecheap.DomainsDNSHostRecordDetailed
	for _, r := range records {
		parkingCNAME := ptr.Deref(r.Type) == namecheap.RecordTypeCNAME &&
			ptr.Deref(r.Name) == "www" &&
			ptr.Deref(r.Address) == "parkingpage.namecheap.com."
		parkingURL := ptr.Deref(r.Type) == namecheap.RecordTypeURL &&
			ptr.Deref(r.Name) == "@" &&
			strings.HasPrefix(ptr.Deref(r.Address), "http://www."+domain)
		if parkingCNAME || parkingURL {
			continue
		}
		out = append(out, r)
	}
	return out
}

// resolveEmailType keeps an MX or MXE email type only while a matching record
// is present, downgrading it to NONE otherwise. It guards the case where the
// last MX or MXE record is removed without the email type being cleared, which
// the API would reject. A nil email type resolves to NONE.
func resolveEmailType(records []namecheap.DomainsDNSHostRecord, emailType *string) *string {
	if emailType == nil || *emailType == "" {
		return namecheap.String(namecheap.EmailTypeNone)
	}
	if *emailType != namecheap.EmailTypeMXE && *emailType != namecheap.EmailTypeMX {
		return emailType
	}
	foundMX, foundMXE := false, false
	for _, rec := range records {
		switch ptr.Deref(rec.RecordType) {
		case namecheap.RecordTypeMX:
			foundMX = true
		case namecheap.RecordTypeMXE:
			foundMXE = true
		}
	}
	if (*emailType == namecheap.EmailTypeMX && !foundMX) ||
		(*emailType == namecheap.EmailTypeMXE && !foundMXE) {
		return namecheap.String(namecheap.EmailTypeNone)
	}
	return emailType
}

// mergeCreateRecords unions the live records with the desired ones, keyed by
// identity hash. A desired record whose hash is already present is a duplicate
// and fails the apply. The result is sorted so a plan renders stably.
func mergeCreateRecords(
	remote []namecheap.DomainsDNSHostRecordDetailed,
	desired []namecheap.DomainsDNSHostRecord,
	domain string,
) ([]namecheap.DomainsDNSHostRecord, error) {
	byHash := map[string]namecheap.DomainsDNSHostRecord{}
	for _, d := range filterParkingRecords(remote, domain) {
		byHash[detailedHash(d)] = hostRecordFromDetailed(d)
	}
	for _, rec := range desired {
		h, err := desiredHash(rec)
		if err != nil {
			return nil, err
		}
		if _, dup := byHash[h]; dup {
			return nil, fmt.Errorf("duplicate record %s already exists", stringifyRecord(rec))
		}
		byHash[h] = rec
	}
	merged := make([]namecheap.DomainsDNSHostRecord, 0, len(byHash))
	for _, rec := range byHash {
		merged = append(merged, rec)
	}
	sortHostRecords(merged)
	return merged, nil
}

// mergeUpdateRecords drops the records named by priorHashes from the live set,
// appends the desired records, and sorts the result. The records the resource
// owned before give way to the ones it owns now, while every other live record
// stays.
func mergeUpdateRecords(
	remote []namecheap.DomainsDNSHostRecordDetailed,
	priorHashes map[string]bool,
	desired []namecheap.DomainsDNSHostRecord,
) []namecheap.DomainsDNSHostRecord {
	var out []namecheap.DomainsDNSHostRecord
	for _, d := range remote {
		if priorHashes[detailedHash(d)] {
			continue
		}
		out = append(out, hostRecordFromDetailed(d))
	}
	out = append(out, desired...)
	sortHostRecords(out)
	return out
}

// sortHostRecords orders records by hostname, then type, then address, so a
// merge writes them in a stable order.
func sortHostRecords(records []namecheap.DomainsDNSHostRecord) {
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if h := strings.Compare(ptr.Deref(a.HostName), ptr.Deref(b.HostName)); h != 0 {
			return h < 0
		}
		if t := strings.Compare(ptr.Deref(a.RecordType), ptr.Deref(b.RecordType)); t != 0 {
			return t < 0
		}
		return ptr.Deref(a.Address) < ptr.Deref(b.Address)
	})
}

// stringifyRecord renders a record's identity for a duplicate-record error.
func stringifyRecord(rec namecheap.DomainsDNSHostRecord) string {
	return fmt.Sprintf("{hostname = %s, type = %s, address = %s}",
		ptr.Deref(rec.HostName), ptr.Deref(rec.RecordType), ptr.Deref(rec.Address))
}

// setHosts writes the full host-record set for a domain with the given email
// type.
func setHosts(
	client *namecheap.Client,
	domain string,
	records []namecheap.DomainsDNSHostRecord,
	emailType *string,
) error {
	_, err := client.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    new(domain),
		Records:   &records,
		EmailType: emailType,
	})
	if err != nil {
		return fmt.Errorf("set hosts: %w", err)
	}
	return nil
}

// setCustom delegates a domain to the given custom nameservers.
func setCustom(client *namecheap.Client, domain string, nameservers []string) error {
	if _, err := client.DomainsDNS.SetCustom(domain, nameservers); err != nil {
		return fmt.Errorf("set custom nameservers: %w", err)
	}
	return nil
}

// setDefault returns a domain to Namecheap's default DNS.
func setDefault(client *namecheap.Client, domain string) error {
	if _, err := client.DomainsDNS.SetDefault(domain); err != nil {
		return fmt.Errorf("set default nameservers: %w", err)
	}
	return nil
}

// ensureOurDNS returns a domain to Namecheap's DNS when it is delegated to
// custom nameservers, so a following host-record write is not rejected. This
// mirrors the Terraform provider's recovery when records are applied to a
// domain a user manually switched to custom nameservers.
func ensureOurDNS(client *namecheap.Client, domain string) error {
	resp, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return fmt.Errorf("get dns list: %w", err)
	}
	ourDNS, err := usingOurDNS(resp)
	if err != nil {
		return err
	}
	if !ourDNS {
		return setDefault(client, domain)
	}
	return nil
}

// hostsOf returns the host records from a GetHosts response, or nil when the
// response carries none.
func hostsOf(
	resp *namecheap.DomainsDNSGetHostsCommandResponse,
) []namecheap.DomainsDNSHostRecordDetailed {
	if resp == nil || resp.DomainDNSGetHostsResult == nil ||
		resp.DomainDNSGetHostsResult.Hosts == nil {
		return nil
	}
	return *resp.DomainDNSGetHostsResult.Hosts
}

// emailTypeOf returns the email type from a GetHosts response, or nil when the
// response carries none.
func emailTypeOf(resp *namecheap.DomainsDNSGetHostsCommandResponse) *string {
	if resp == nil || resp.DomainDNSGetHostsResult == nil {
		return nil
	}
	return resp.DomainDNSGetHostsResult.EmailType
}

// usingOurDNS reports whether a domain resolves through Namecheap's own DNS. A
// response missing the flag means Namecheap returned no usable DNS state, which
// happens when the domain is not on the account.
func usingOurDNS(resp *namecheap.DomainsDNSGetListCommandResponse) (bool, error) {
	if resp == nil || resp.DomainDNSGetListResult == nil ||
		resp.DomainDNSGetListResult.IsUsingOurDNS == nil {
		return false, fmt.Errorf(
			"namecheap returned no dns state for the domain; it may not exist on the account")
	}
	return *resp.DomainDNSGetListResult.IsUsingOurDNS, nil
}

// nameserversOf returns the nameservers from a GetList response, or nil when the
// response carries none.
func nameserversOf(resp *namecheap.DomainsDNSGetListCommandResponse) []string {
	if resp == nil || resp.DomainDNSGetListResult == nil ||
		resp.DomainDNSGetListResult.Nameservers == nil {
		return nil
	}
	return *resp.DomainDNSGetListResult.Nameservers
}

// containsFold reports whether list holds target, comparing case-insensitively
// as Namecheap does for nameservers.
func containsFold(list []string, target string) bool {
	for _, item := range list {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
}
