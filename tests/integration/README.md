# Integration tests

These tests drive the library against the **real Namecheap API**. They create
and tear down real DNS state on a domain you own, so run them only against a
disposable test domain.

Each scenario is compiled with unobin, then put through create, verify, update,
and destroy. A `verify` program reads the live state through the Namecheap API
and asserts it matches the scenario; destroy runs even after a failure so a run
does not leave records or a custom nameserver delegation behind.

## Requirements

- A Namecheap account with API access enabled, and the calling IP whitelisted.
- A disposable domain on the account named by `NAMECHEAP_TEST_DOMAIN`.
- Docker (the `make` target runs the suite in a container).

## Environment

| Variable | Required | Meaning |
| --- | --- | --- |
| `NAMECHEAP_USER_NAME` | yes | Namecheap account username |
| `NAMECHEAP_API_USER` | yes | API user (usually the same) |
| `NAMECHEAP_API_KEY` | yes | API key |
| `NAMECHEAP_TEST_DOMAIN` | yes | Disposable domain to manage and tear down |
| `NAMECHEAP_CLIENT_IP` | no | Whitelisted client IP (default `0.0.0.0`) |
| `NAMECHEAP_USE_SANDBOX` | no | `true` to use the sandbox API |

## Running

```sh
make test-integration-live
```

Run one scenario with `SCENARIO`:

```sh
SCENARIO=records make test-integration-live
```

## Scenarios

- **records** — manages host records (a www A record and a txt TXT record) in
  overwrite mode. Overwrite owns the whole record set, so this replaces the
  domain's records and clears them on destroy.
- **nameservers** — delegates the domain to a pair of custom nameservers in
  overwrite mode, and returns it to Namecheap's default DNS on destroy. The
  placeholder nameservers (`ns1.example.com`, `ns2.example.com`) are stored as
  given; change them to nameservers your registrar accepts if Namecheap rejects
  them.
