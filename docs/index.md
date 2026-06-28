# Namecheap library

The Namecheap library manages DNS records and custom nameserver delegation for
Namecheap domains. Import it in factory source and pass one Namecheap
configuration value to the import alias.

```
factory: {
  description: 'Creates one DNS record.'

  inputs: {
    namecheap-config: {
      type: library-config('github.com/cloudboss/unobin-library-namecheap')
      default: { use-sandbox: true }
    }
    domain: { type: string }
  }

  imports: { namecheap: 'github.com/cloudboss/unobin-library-namecheap' }

  library-configs: {
    namecheap: input.namecheap-config
  }

  resources: {
    www: namecheap.domain-records {
      domain: input.domain
      records: [{
        hostname: 'www'
        type: 'CNAME'
        address: '@'
      }]
    }
  }

  outputs: {
    record-count: { value: @core.length(resource.www.records) }
  }
}
```

Add the library to the dependency project before compiling the factory:

```
unobin deps get github.com/cloudboss/unobin-library-namecheap@v0.4.0
```

## Configuration

Configuration uses Namecheap API credentials and endpoint settings. Every
credential field can be set in the library configuration or through the matching
environment variable.

| Field | Environment variable |
| --- | --- |
| `user-name` | `NAMECHEAP_USER_NAME` |
| `api-user` | `NAMECHEAP_API_USER` |
| `api-key` | `NAMECHEAP_API_KEY` |
| `client-ip` | `NAMECHEAP_CLIENT_IP` |
| `use-sandbox` | `NAMECHEAP_USE_SANDBOX` |

`client-ip` defaults to `0.0.0.0` when it is unset. Set `use-sandbox: true` for
the Namecheap sandbox. `base-url` is normally unset; it is available for tests
or a compatible proxy.

See [configuration reference](reference/configuration.md) for every field.

## DNS records

`namecheap.domain-records` manages the host records and email type for a domain.
Namecheap writes DNS records as a complete domain record set, so the resource has
two modes:

- `MERGE`, the default, owns only the listed records and preserves other records.
- `OVERWRITE` owns the full record set and removes records not declared by the
  resource.

```
resources: {
  website-records: namecheap.domain-records {
    domain: 'example.com'
    mode: 'MERGE'
    records: [
      {
        hostname: '@'
        type: 'A'
        address: '203.0.113.10'
        ttl: 1800
      }
      {
        hostname: 'www'
        type: 'CNAME'
        address: '@'
      }
    ]
  }
}
```

When a domain is delegated to custom nameservers, Namecheap host records are not
active. Applying `domain-records` returns the domain to Namecheap DNS before
writing records.

## Nameservers

`namecheap.domain-nameservers` delegates a domain to custom nameservers. A custom
delegation disables Namecheap host records for that domain, so do not manage the
same domain with `domain-records` and `domain-nameservers` at the same time.

```
resources: {
  delegation: namecheap.domain-nameservers {
    domain: 'example.com'
    mode: 'OVERWRITE'
    nameservers: [
      'ns1.example.net'
      'ns2.example.net'
    ]
  }
}
```

`MERGE` adds the listed nameservers to any existing custom delegation and removes
only the nameservers it owns on delete. `OVERWRITE` owns the full delegation and
returns the domain to Namecheap DNS on delete.

## Reference

The generated reference lists every resource kind, its inputs, outputs, defaults,
constraints, and sensitive fields.

- [Configuration](reference/configuration.md)
- [Resources](reference/resources/index.md)
