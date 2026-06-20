// Package library exports the unobin registration record for the Namecheap
// library. Library returns the resources and configuration the library
// provides, keyed by the names a stack uses to reference them.
package library

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"

	"github.com/cloudboss/unobin-library-namecheap/internal/config"
	"github.com/cloudboss/unobin-library-namecheap/internal/service/domain"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "namecheap",
		Description: "Namecheap library for unobin.",
		Configuration: &cfg.ConfigurationType[*config.Configuration]{
			Description: "Namecheap library configuration",
			New:         func() *config.Configuration { return &config.Configuration{} },
		},
		Resources: map[string]runtime.ResourceRegistration{
			"domain-records": runtime.MakeResource[
				domain.DomainRecords,
				*domain.DomainRecordsOutput,
				*config.Configuration,
			](),
			"domain-nameservers": runtime.MakeResource[
				domain.DomainNameservers,
				*domain.DomainNameserversOutput,
				*config.Configuration,
			](),
		},
	}
}
