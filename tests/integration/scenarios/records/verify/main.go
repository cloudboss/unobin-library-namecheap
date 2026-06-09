// verify checks the host records the records scenario manages against the phase
// named in the VERIFY_PHASE environment variable. Applied requires the www A
// record present with the first apply's address and the txt TXT record present;
// destroyed requires the www A record gone. It only reads Namecheap state;
// tearing the records down is the destroy plan's job.
package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

const (
	wwwHost    = "www"
	wwwAddress = "192.0.2.1"
	txtHost    = "txt"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	domain := os.Getenv("NAMECHEAP_TEST_DOMAIN")
	if domain == "" {
		return fmt.Errorf("NAMECHEAP_TEST_DOMAIN is required")
	}
	resp, err := newClient().DomainsDNS.GetHosts(domain)
	if err != nil {
		return fmt.Errorf("get hosts: %w", err)
	}
	hosts := hostsOf(resp)

	switch phase := os.Getenv("VERIFY_PHASE"); phase {
	case "applied":
		www := findHost(hosts, wwwHost, namecheap.RecordTypeA)
		if www == nil {
			return fmt.Errorf("www A record not found on %s", domain)
		}
		if got := deref(www.Address); got != wwwAddress {
			return fmt.Errorf("www A address is %q, want %q", got, wwwAddress)
		}
		if findHost(hosts, txtHost, namecheap.RecordTypeTXT) == nil {
			return fmt.Errorf("txt TXT record not found on %s", domain)
		}
		fmt.Printf("ok: www and txt records present on %s\n", domain)
		return nil
	case "destroyed":
		if findHost(hosts, wwwHost, namecheap.RecordTypeA) != nil {
			return fmt.Errorf("www A record still present on %s", domain)
		}
		fmt.Printf("ok: managed records gone on %s\n", domain)
		return nil
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func newClient() *namecheap.Client {
	return namecheap.NewClient(&namecheap.ClientOptions{
		UserName:   os.Getenv("NAMECHEAP_USER_NAME"),
		ApiUser:    os.Getenv("NAMECHEAP_API_USER"),
		ApiKey:     os.Getenv("NAMECHEAP_API_KEY"),
		ClientIp:   getenvOr("NAMECHEAP_CLIENT_IP", "0.0.0.0"),
		UseSandbox: strings.EqualFold(os.Getenv("NAMECHEAP_USE_SANDBOX"), "true"),
	})
}

func hostsOf(
	resp *namecheap.DomainsDNSGetHostsCommandResponse,
) []namecheap.DomainsDNSHostRecordDetailed {
	if resp == nil || resp.DomainDNSGetHostsResult == nil ||
		resp.DomainDNSGetHostsResult.Hosts == nil {
		return nil
	}
	return *resp.DomainDNSGetHostsResult.Hosts
}

func findHost(
	hosts []namecheap.DomainsDNSHostRecordDetailed, name, recordType string,
) *namecheap.DomainsDNSHostRecordDetailed {
	for i := range hosts {
		if strings.EqualFold(deref(hosts[i].Name), name) &&
			strings.EqualFold(deref(hosts[i].Type), recordType) {
			return &hosts[i]
		}
	}
	return nil
}

func getenvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
