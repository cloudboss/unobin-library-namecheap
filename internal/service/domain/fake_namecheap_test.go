package domain

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"

	"github.com/cloudboss/unobin-library-namecheap/internal/config"
)

// fakeNamecheap is a stateful, in-process stand-in for the Namecheap DNS API.
// It models each domain's DNS state -- whether it resolves through Namecheap,
// its email type, its host records, and its custom nameservers -- and mutates
// that state on writes the way the real API does, so a read after a write
// reflects what was written. Every request form is recorded so a test can
// assert on what was sent.
type fakeNamecheap struct {
	t        *testing.T
	mu       sync.Mutex
	domains  map[string]*fakeDomain
	requests []url.Values
	server   *httptest.Server
}

// fakeDomain is one domain's DNS state inside the fake.
type fakeDomain struct {
	usingOurDNS bool
	emailType   string
	hosts       []fakeHost
	nameservers []string
}

// fakeHost is one host record inside the fake.
type fakeHost struct {
	id      int
	name    string
	rtype   string
	address string
	mxPref  int
	ttl     int
}

func newFakeNamecheap(t *testing.T) *fakeNamecheap {
	f := &fakeNamecheap{t: t, domains: map[string]*fakeDomain{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

// seed sets a domain's starting DNS state.
func (f *fakeNamecheap) seed(domain string, d fakeDomain) {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := d
	f.domains[domain] = &state
}

// state returns a snapshot of a domain's current DNS state.
func (f *fakeNamecheap) state(domain string) fakeDomain {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d := f.domains[domain]; d != nil {
		return *d
	}
	return fakeDomain{}
}

// sent returns the request forms recorded for one Namecheap command.
func (f *fakeNamecheap) sent(command string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []url.Values
	for _, form := range f.requests {
		if form.Get("Command") == command {
			out = append(out, form)
		}
	}
	return out
}

// configuration returns a library configuration pointing the SDK at the fake
// server, and clears the NAMECHEAP_* environment so the test is hermetic.
func (f *fakeNamecheap) configuration() *config.Configuration {
	for _, k := range []string{
		"NAMECHEAP_USER_NAME", "NAMECHEAP_API_USER", "NAMECHEAP_API_KEY",
		"NAMECHEAP_CLIENT_IP", "NAMECHEAP_USE_SANDBOX",
	} {
		f.t.Setenv(k, "")
	}
	return &config.Configuration{
		UserName: &cfg.String{Value: "user"},
		APIUser:  &cfg.String{Value: "user"},
		APIKey:   &cfg.String{Value: "key"},
		BaseURL:  &cfg.String{Value: f.server.URL},
	}
}

func (f *fakeNamecheap) serve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		f.t.Errorf("fake namecheap: parse form: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	form := r.PostForm
	domain := form.Get("SLD") + "." + form.Get("TLD")

	f.mu.Lock()
	f.requests = append(f.requests, form)
	d := f.domain(domain)
	var body []byte
	switch command := form.Get("Command"); command {
	case "namecheap.domains.dns.getList":
		body = f.getListBody(d, domain)
	case "namecheap.domains.dns.getHosts":
		body = f.getHostsBody(d, domain)
	case "namecheap.domains.dns.setHosts":
		body = f.setHostsBody(d, domain, form)
	case "namecheap.domains.dns.setCustom":
		body = f.setCustomBody(d, domain, form)
	case "namecheap.domains.dns.setDefault":
		body = f.setDefaultBody(d, domain)
	default:
		f.mu.Unlock()
		f.t.Errorf("fake namecheap: unexpected command %q", command)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.mu.Unlock()

	w.Header().Set("Content-Type", "text/xml")
	if _, err := w.Write(body); err != nil {
		f.t.Errorf("fake namecheap: write: %v", err)
	}
}

// domain returns a domain's state, creating a default (Namecheap DNS, no email
// type) one for a domain a test did not seed.
func (f *fakeNamecheap) domain(domain string) *fakeDomain {
	d := f.domains[domain]
	if d == nil {
		d = &fakeDomain{usingOurDNS: true, emailType: namecheap.EmailTypeNone}
		f.domains[domain] = d
	}
	return d
}

func (f *fakeNamecheap) getListBody(d *fakeDomain, domain string) []byte {
	var nameservers *[]string
	if len(d.nameservers) > 0 {
		ns := append([]string{}, d.nameservers...)
		nameservers = &ns
	}
	return f.marshal(namecheap.DomainsDNSGetListResponse{
		XMLName: &xml.Name{Local: "ApiResponse"},
		CommandResponse: &namecheap.DomainsDNSGetListCommandResponse{
			DomainDNSGetListResult: &namecheap.DomainDNSGetListResult{
				Domain:        new(domain),
				IsUsingOurDNS: new(d.usingOurDNS),
				Nameservers:   nameservers,
			},
		},
	})
}

func (f *fakeNamecheap) getHostsBody(d *fakeDomain, domain string) []byte {
	hosts := make([]namecheap.DomainsDNSHostRecordDetailed, 0, len(d.hosts))
	for _, h := range d.hosts {
		hosts = append(hosts, namecheap.DomainsDNSHostRecordDetailed{
			HostId:  new(h.id),
			Name:    new(h.name),
			Type:    new(h.rtype),
			Address: new(h.address),
			MXPref:  new(h.mxPref),
			TTL:     new(h.ttl),
		})
	}
	return f.marshal(namecheap.DomainsDNSGetHostsResponse{
		XMLName: xml.Name{Local: "ApiResponse"},
		CommandResponse: &namecheap.DomainsDNSGetHostsCommandResponse{
			DomainDNSGetHostsResult: &namecheap.DomainDNSGetHostsResult{
				Domain:        new(domain),
				EmailType:     new(d.emailType),
				IsUsingOurDNS: new(d.usingOurDNS),
				Hosts:         &hosts,
			},
		},
	})
}

func (f *fakeNamecheap) setHostsBody(d *fakeDomain, domain string, form url.Values) []byte {
	d.hosts = parseSentRecords(form)
	d.emailType = form.Get("EmailType")
	d.usingOurDNS = true
	return f.marshal(namecheap.DomainsDNSSetHostsResponse{
		XMLName: &xml.Name{Local: "ApiResponse"},
		CommandResponse: &namecheap.DomainsDNSSetHostsCommandResponse{
			DomainDNSSetHostsResult: &namecheap.DomainDNSSetHostsResult{
				Domain:    new(domain),
				IsSuccess: new(true),
			},
		},
	})
}

func (f *fakeNamecheap) setCustomBody(d *fakeDomain, domain string, form url.Values) []byte {
	var nameservers []string
	if raw := form.Get("Nameservers"); raw != "" {
		nameservers = strings.Split(raw, ",")
	}
	d.nameservers = nameservers
	d.usingOurDNS = false
	return f.marshal(namecheap.DomainsDNSSetCustomResponse{
		XMLName: &xml.Name{Local: "ApiResponse"},
		CommandResponse: &namecheap.DomainsDNSSetCustomCommandResponse{
			DomainDNSSetCustomResult: &namecheap.DomainsDNSSetCustomResult{
				Domain:  new(domain),
				Updated: new(true),
			},
		},
	})
}

func (f *fakeNamecheap) setDefaultBody(d *fakeDomain, domain string) []byte {
	d.usingOurDNS = true
	d.nameservers = nil
	return f.marshal(namecheap.DomainsDNSSetDefaultResponse{
		XMLName: &xml.Name{Local: "ApiResponse"},
		CommandResponse: &namecheap.DomainsDNSSetDefaultCommandResponse{
			DomainDNSSetDefaultResult: &namecheap.DomainDNSSetDefaultResult{
				Domain:  new(domain),
				Updated: new(true),
			},
		},
	})
}

func (f *fakeNamecheap) marshal(v any) []byte {
	b, err := xml.Marshal(v)
	if err != nil {
		f.t.Fatalf("fake namecheap: marshal: %v", err)
	}
	return b
}

// parseSentRecords reconstructs the host records from a setHosts request form,
// where each record's fields are suffixed with its 1-based index.
func parseSentRecords(form url.Values) []fakeHost {
	var hosts []fakeHost
	for i := 1; ; i++ {
		idx := strconv.Itoa(i)
		rtype := form.Get("RecordType" + idx)
		if rtype == "" {
			break
		}
		ttl, _ := strconv.Atoi(form.Get("TTL" + idx))
		mxPref, _ := strconv.Atoi(form.Get("MXPref" + idx))
		hosts = append(hosts, fakeHost{
			id:      i,
			name:    form.Get("HostName" + idx),
			rtype:   rtype,
			address: form.Get("Address" + idx),
			mxPref:  mxPref,
			ttl:     ttl,
		})
	}
	return hosts
}

// sentRecord is a host record extracted from a setHosts request form for
// order-independent assertions.
type sentRecord struct {
	Hostname string
	Type     string
	Address  string
	MXPref   string
	TTL      string
}

// recordsFromForm extracts the host records a setHosts request carried.
func recordsFromForm(form url.Values) []sentRecord {
	var records []sentRecord
	for i := 1; ; i++ {
		idx := strconv.Itoa(i)
		rtype := form.Get("RecordType" + idx)
		if rtype == "" {
			break
		}
		records = append(records, sentRecord{
			Hostname: form.Get("HostName" + idx),
			Type:     rtype,
			Address:  form.Get("Address" + idx),
			MXPref:   form.Get("MXPref" + idx),
			TTL:      form.Get("TTL" + idx),
		})
	}
	return records
}
