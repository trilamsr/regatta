# regatta CLI flags

Operator-facing reference for `regatta serve` flags. One entry per flag; each
entry explains the use case before showing the example so a reader who lands
here from `--help` understands WHY before HOW.

## `regatta serve`

### `--public-url`

External URL operators reach the listener via. Required when `regatta serve`
runs behind a reverse proxy (Caddy, nginx, Cloudflare Tunnel, k8s Ingress) so
the inner `r.Host` header (pod-side hostname) does not break the POST
`Origin`-header check.

Without `--public-url`, the listener's CSRF/origin gate compares the request
`Origin` against `https://` + `r.Host`. Behind a proxy, `r.Host` carries the
inner pod hostname (e.g. `pod-inner-1.svc:8080`) — not the public hostname
(`regatta.example.com`) operators actually type into the browser or Slack
button. The gate then rejects every approval POST as a foreign Origin. Setting
`--public-url` pins the public hostname so the comparison succeeds (#304).

Accepted form: a full URL with an `http://` or `https://` scheme. Bare
hostnames are rejected at boot to keep mis-configurations loud.

Example:

```
regatta serve --public-url https://regatta.example.com
```

Combine with `--addr 127.0.0.1:8080` when the proxy terminates TLS and forwards
plaintext to the listener bound to loopback.
