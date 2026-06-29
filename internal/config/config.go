// Package config holds the Namecheap library configuration and a helper
// that builds a go-namecheap-sdk client from it.
package config

import (
	"os"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"

	"github.com/cloudboss/unobin-library-namecheap/internal/ptr"
)

// defaultClientIP is the client IP Namecheap accepts when the caller pins
// none. The API requires the field, but this placeholder works for accounts
// whose API access is not restricted to specific source addresses. It matches
// the Terraform provider's default.
const defaultClientIP = "0.0.0.0"

// Configuration is the Namecheap library's configuration. Every credential
// field falls back to its NAMECHEAP_* environment variable when left empty, so
// a stack may supply credentials inline or through the environment. BaseURL
// overrides the endpoint the SDK derives from UseSandbox; it exists for tests
// and for pointing at a compatible proxy, and is normally left unset.
type Configuration struct {
	UserName   *string `ub:"user-name"`
	APIUser    *string `ub:"api-user"`
	APIKey     *string `ub:"api-key,sensitive"`
	ClientIP   *string `ub:"client-ip"`
	UseSandbox *bool   `ub:"use-sandbox"`
	BaseURL    *string `ub:"base-url"`
}

// NewClient builds a Namecheap API client from c. A nil c, or any field left
// empty, draws from the NAMECHEAP_* environment variables; an unset client IP
// settles on the placeholder default. A non-empty BaseURL replaces the
// production or sandbox endpoint the SDK would otherwise pick.
func NewClient(c *Configuration) *namecheap.Client {
	var (
		userName, apiUser, apiKey, clientIP, baseURL string
		useSandbox                                   bool
	)
	if c != nil {
		userName = ptr.Deref(c.UserName)
		apiUser = ptr.Deref(c.APIUser)
		apiKey = ptr.Deref(c.APIKey)
		clientIP = ptr.Deref(c.ClientIP)
		useSandbox = ptr.Deref(c.UseSandbox)
		baseURL = ptr.Deref(c.BaseURL)
	}
	userName = orEnv(userName, "NAMECHEAP_USER_NAME")
	apiUser = orEnv(apiUser, "NAMECHEAP_API_USER")
	apiKey = orEnv(apiKey, "NAMECHEAP_API_KEY")
	clientIP = orEnv(clientIP, "NAMECHEAP_CLIENT_IP")
	if clientIP == "" {
		clientIP = defaultClientIP
	}
	if !useSandbox && isTrue(os.Getenv("NAMECHEAP_USE_SANDBOX")) {
		useSandbox = true
	}

	client := namecheap.NewClient(&namecheap.ClientOptions{
		UserName:   userName,
		ApiUser:    apiUser,
		ApiKey:     apiKey,
		ClientIp:   clientIP,
		UseSandbox: useSandbox,
	})
	if baseURL != "" {
		client.BaseURL = baseURL
	}
	return client
}

// orEnv returns value when it is non-empty, otherwise the named environment
// variable.
func orEnv(value, key string) string {
	if value != "" {
		return value
	}
	return os.Getenv(key)
}

// isTrue reports whether an environment value reads as enabled.
func isTrue(v string) bool {
	switch v {
	case "1", "t", "T", "true", "TRUE", "True":
		return true
	default:
		return false
	}
}
