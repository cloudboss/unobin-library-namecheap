package library_test

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/require"

	library "github.com/cloudboss/unobin-library-namecheap"
)

const libraryPath = "github.com/cloudboss/unobin-library-namecheap"

type testEncrypter struct{}

func (testEncrypter) Encrypt(plaintext []byte) ([]byte, error)  { return plaintext, nil }
func (testEncrypter) Decrypt(ciphertext []byte) ([]byte, error) { return ciphertext, nil }
func (testEncrypter) Describe() encrypt.Description {
	return encrypt.Description{KeySource: "test"}
}

type fakeAPIRecord struct {
	name     string
	typeName string
	address  string
}

type fakeNamecheapAPI struct {
	t          *testing.T
	mu         sync.Mutex
	records    map[string][]fakeAPIRecord
	requests   []url.Values
	allowedKey string
	server     *httptest.Server
}

func newFakeNamecheapAPI(t *testing.T) *fakeNamecheapAPI {
	t.Helper()
	fake := &fakeNamecheapAPI{t: t, records: map[string][]fakeAPIRecord{}}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeNamecheapAPI) URL() string {
	return f.server.URL
}

func (f *fakeNamecheapAPI) setAllowedKey(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allowedKey = key
}

func (f *fakeNamecheapAPI) domainRecords(domain string) []fakeAPIRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeAPIRecord{}, f.records[domain]...)
}

func (f *fakeNamecheapAPI) mutationCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, request := range f.requests {
		if request.Get("Command") == "namecheap.domains.dns.setHosts" {
			count++
		}
	}
	return count
}

func (f *fakeNamecheapAPI) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	form := r.PostForm
	domain := form.Get("SLD") + "." + form.Get("TLD")

	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, form)
	if f.allowedKey != "" && form.Get("ApiKey") != f.allowedKey {
		http.Error(w, "invalid API key", http.StatusForbidden)
		return
	}

	var response any
	switch command := form.Get("Command"); command {
	case "namecheap.domains.dns.getList":
		response = namecheap.DomainsDNSGetListResponse{
			XMLName: &xml.Name{Local: "ApiResponse"},
			CommandResponse: &namecheap.DomainsDNSGetListCommandResponse{
				DomainDNSGetListResult: &namecheap.DomainDNSGetListResult{
					Domain:        new(domain),
					IsUsingOurDNS: new(true),
				},
			},
		}
	case "namecheap.domains.dns.getHosts":
		hosts := make([]namecheap.DomainsDNSHostRecordDetailed, 0, len(f.records[domain]))
		for i, record := range f.records[domain] {
			hosts = append(hosts, namecheap.DomainsDNSHostRecordDetailed{
				HostId:  new(i + 1),
				Name:    new(record.name),
				Type:    new(record.typeName),
				Address: new(record.address),
				MXPref:  new(10),
				TTL:     new(1800),
			})
		}
		response = namecheap.DomainsDNSGetHostsResponse{
			XMLName: xml.Name{Local: "ApiResponse"},
			CommandResponse: &namecheap.DomainsDNSGetHostsCommandResponse{
				DomainDNSGetHostsResult: &namecheap.DomainDNSGetHostsResult{
					Domain:        new(domain),
					EmailType:     new(namecheap.EmailTypeNone),
					IsUsingOurDNS: new(true),
					Hosts:         &hosts,
				},
			},
		}
	case "namecheap.domains.dns.setHosts":
		f.records[domain] = fakeRecordsFromForm(form)
		response = namecheap.DomainsDNSSetHostsResponse{
			XMLName: &xml.Name{Local: "ApiResponse"},
			CommandResponse: &namecheap.DomainsDNSSetHostsCommandResponse{
				DomainDNSSetHostsResult: &namecheap.DomainDNSSetHostsResult{
					Domain:    new(domain),
					IsSuccess: new(true),
				},
			},
		}
	default:
		http.Error(w, "unexpected command", http.StatusBadRequest)
		return
	}

	body, err := xml.Marshal(response)
	if err != nil {
		f.t.Errorf("marshal fake Namecheap response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/xml")
	if _, err := w.Write(body); err != nil {
		f.t.Errorf("write fake Namecheap response: %v", err)
	}
}

func fakeRecordsFromForm(form url.Values) []fakeAPIRecord {
	var records []fakeAPIRecord
	for i := 1; ; i++ {
		index := strconv.Itoa(i)
		typeName := form.Get("RecordType" + index)
		if typeName == "" {
			return records
		}
		records = append(records, fakeAPIRecord{
			name:     form.Get("HostName" + index),
			typeName: typeName,
			address:  form.Get("Address" + index),
		})
	}
}

func consumerInputs(domain, recordType, address, baseURL, apiKey string) map[string]any {
	return map[string]any{
		"domain":      domain,
		"record-type": recordType,
		"address":     address,
		"namecheap-config": map[string]any{
			"user-name": "user",
			"api-user":  "user",
			"api-key":   apiKey,
			"base-url":  baseURL,
		},
	}
}

func consumerExecutor(
	t *testing.T,
	stateDir string,
	inputs map[string]any,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join(
		"testdata", "ub", "compiled", "valid", "replacement", "src", "factory.ub",
	)
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := syntax.ParseSource(path, source)
	require.NoError(t, err)
	require.NotNil(t, parsed.Factory)

	registered := runtime.LibraryWithPath(library.Library(), libraryPath)
	libraries := map[string]*runtime.Library{"namecheap": registered}
	store, err := local.NewStore(stateDir, "consumer", "test", testEncrypter{})
	require.NoError(t, err)
	body := &parsed.Factory.Body
	return &runtime.Executor{
		DAG:          runtime.BuildSyntaxDAG(*body, libraries),
		SyntaxSource: body,
		Libraries:    libraries,
		Inputs:       inputs,
		Store:        store,
		Factory: state.FactoryInfo{
			Name: "consumer", Version: "test", ContentRevision: "test",
		},
	}
}

func savedPlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	decoded, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	require.Equal(t, runtime.PlanFormatVersion, decoded.FormatVersion)
	return decoded
}

func resourceStep(
	t *testing.T,
	plan *runtime.PlanFile,
	decision runtime.Decision,
) *runtime.PlanStep {
	t.Helper()
	for i := range plan.Steps {
		step := &plan.Steps[i]
		if step.Address == "resource.main" {
			require.Equal(t, decision, step.Decision)
			return step
		}
	}
	t.Fatal("missing resource.main plan step")
	return nil
}

func applySavedPlan(
	t *testing.T,
	executor *runtime.Executor,
	plan *runtime.PlanFile,
) *state.Snapshot {
	t.Helper()
	result, err := executor.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	require.NotEmpty(t, result.WrittenRev)
	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	require.NoError(t, snapshot.Validate())
	require.Equal(t, state.CurrentFormatVersion, snapshot.FormatVersion)
	return snapshot
}

func requirePriorEntry(t *testing.T, executor *runtime.Executor, prior *state.Snapshot) {
	t.Helper()
	current, err := executor.Store.Current()
	require.NoError(t, err)
	require.Equal(t, prior.Find("resource.main"), current.Find("resource.main"))
}
