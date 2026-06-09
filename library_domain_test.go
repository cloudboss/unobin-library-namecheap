package library_test

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	library "github.com/cloudboss/unobin-library-namecheap"
	"github.com/cloudboss/unobin-library-namecheap/internal/service/domain"
)

// TestLibraryRegistersDomainResources checks the runtime registration: both
// domain resources are present under Resources and dispatch to their output
// type.
func TestLibraryRegistersDomainResources(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"domain-records":     reflect.TypeFor[*domain.DomainRecordsOutput](),
		"domain-nameservers": reflect.TypeFor[*domain.DomainNameserversOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestDomainSchemas asserts the whole derived TypeSchema for each domain
// resource: input and output field types, the cross-field and enum constraints
// each Constraints method declares, and the declared defaults.
func TestDomainSchemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	recordFields := []typecheck.ObjectField{
		{Name: "hostname", Type: typecheck.TString()},
		{Name: "type", Type: typecheck.TString()},
		{Name: "address", Type: typecheck.TString()},
	}

	cases := map[string]*runtime.TypeSchema{
		"domain-records": {
			Inputs: map[string]typecheck.Type{
				"domain":     typecheck.TString(),
				"email-type": typecheck.TOptional(typecheck.TString()),
				"mode":       typecheck.TString(),
				"records": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "hostname", Type: typecheck.TString()},
					{Name: "type", Type: typecheck.TString()},
					{Name: "address", Type: typecheck.TString()},
					{Name: "mx-pref", Type: typecheck.TInteger(), Optional: true},
					{Name: "ttl", Type: typecheck.TInteger(), Optional: true},
				})),
			},
			Outputs: map[string]typecheck.Type{
				"domain":     typecheck.TString(),
				"email-type": typecheck.TString(),
				"mode":       typecheck.TString(),
				"records":    typecheck.TList(typecheck.TObject(recordFields)),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:    "predicate",
					When:    "true",
					Require: "((var.domain != null) && (@core.length(var.domain) >= 1))",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(var.mode == 'MERGE' || var.mode == 'OVERWRITE')",
				},
				{
					Kind: "predicate",
					When: "(var.email-type != null)",
					Require: "(var.email-type == 'NONE' || var.email-type == 'MXE' || " +
						"var.email-type == 'MX' || var.email-type == 'FWD' || " +
						"var.email-type == 'OX' || var.email-type == 'GMAIL')",
					Message: "email-type must be one of NONE, MXE, MX, FWD, OX, or GMAIL",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value.type == 'A' || @each.value.type == 'AAAA' || " +
						"@each.value.type == 'ALIAS' || @each.value.type == 'CAA' || " +
						"@each.value.type == 'CNAME' || @each.value.type == 'MX' || " +
						"@each.value.type == 'MXE' || @each.value.type == 'NS' || " +
						"@each.value.type == 'TXT' || @each.value.type == 'URL' || " +
						"@each.value.type == 'URL301' || @each.value.type == 'FRAME')",
					Message: "a record type must be a valid Namecheap record type",
					ForEach: "var.records",
				},
				{
					Kind: "predicate",
					When: "(@each.value.ttl != null)",
					Require: "(@each.value.ttl == null || @each.value.ttl >= 60) && " +
						"(@each.value.ttl == null || @each.value.ttl <= 60000)",
					Message: "a record ttl must be between 60 and 60000",
					ForEach: "var.records",
				},
				{
					Kind: "predicate",
					When: "(@each.value.mx-pref != null)",
					Require: "(@each.value.mx-pref == null || @each.value.mx-pref >= 0) && " +
						"(@each.value.mx-pref == null || @each.value.mx-pref <= 255)",
					Message: "a record mx-pref must be between 0 and 255",
					ForEach: "var.records",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.mode", Value: "'MERGE'"},
				{Field: "var.records", Optional: true},
			},
		},
		"domain-nameservers": {
			Inputs: map[string]typecheck.Type{
				"domain":      typecheck.TString(),
				"mode":        typecheck.TString(),
				"nameservers": typecheck.TList(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"domain":      typecheck.TString(),
				"mode":        typecheck.TString(),
				"nameservers": typecheck.TList(typecheck.TString()),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:    "predicate",
					When:    "true",
					Require: "((var.domain != null) && (@core.length(var.domain) >= 1))",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(var.mode == 'MERGE' || var.mode == 'OVERWRITE')",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(var.nameservers == null || @core.length(var.nameservers) >= 2)",
					Message: "a domain must have at least 2 nameservers",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.mode", Value: "'MERGE'"},
			},
		},
	}

	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assert.Equal(t, want, schema.Resources[key])
		})
	}
}
