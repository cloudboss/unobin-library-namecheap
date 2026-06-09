// verify checks the custom nameserver delegation the nameservers scenario
// manages against the phase named in the VERIFY_PHASE environment variable.
// Applied requires the domain on custom nameservers with the first apply's pair
// present; destroyed requires it back on Namecheap's default DNS. It only reads
// Namecheap state; tearing the delegation down is the destroy plan's job.
package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

var wantNameservers = []string{"ns1.example.com", "ns2.example.com"}

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
	resp, err := newClient().DomainsDNS.GetList(domain)
	if err != nil {
		return fmt.Errorf("get dns list: %w", err)
	}
	if resp == nil || resp.DomainDNSGetListResult == nil ||
		resp.DomainDNSGetListResult.IsUsingOurDNS == nil {
		return fmt.Errorf("namecheap returned no dns state for %s", domain)
	}
	usingOurDNS := *resp.DomainDNSGetListResult.IsUsingOurDNS

	switch phase := os.Getenv("VERIFY_PHASE"); phase {
	case "applied":
		if usingOurDNS {
			return fmt.Errorf("%s is still on Namecheap DNS; custom nameservers not set", domain)
		}
		nameservers := nameserversOf(resp)
		for _, want := range wantNameservers {
			if !containsFold(nameservers, want) {
				return fmt.Errorf("nameserver %s not delegated on %s (have %v)",
					want, domain, nameservers)
			}
		}
		fmt.Printf("ok: %s delegated to %v\n", domain, nameservers)
		return nil
	case "destroyed":
		if !usingOurDNS {
			return fmt.Errorf("%s is still on custom nameservers", domain)
		}
		fmt.Printf("ok: %s back on Namecheap default DNS\n", domain)
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

func nameserversOf(resp *namecheap.DomainsDNSGetListCommandResponse) []string {
	if resp == nil || resp.DomainDNSGetListResult == nil ||
		resp.DomainDNSGetListResult.Nameservers == nil {
		return nil
	}
	return *resp.DomainDNSGetListResult.Nameservers
}

func containsFold(list []string, target string) bool {
	for _, item := range list {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
}

func getenvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
