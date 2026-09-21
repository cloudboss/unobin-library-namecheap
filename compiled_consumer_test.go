package library_test

import (
	"fmt"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
)

func TestCompiledConsumer(t *testing.T) {
	fake := newFakeNamecheapAPI(t)
	e2etest.RunCompiledCases(t, "testdata/ub/compiled/valid",
		e2etest.WithGoModule(libraryPath, "."),
		e2etest.WithEnv(map[string]string{
			"NAMECHEAP_USER_NAME": "user",
			"NAMECHEAP_API_USER":  "user",
			"NAMECHEAP_API_KEY":   "current-key",
			"UB_INPUT_namecheap_config": fmt.Sprintf(
				"{base-url: '%s'}", fake.URL(),
			),
		}),
	)
}
