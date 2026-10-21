# Enois Cloud API Gateway

Reverse gateway in Go for Cloud/ML services. The example configuration exposes
a moderation API that sends text and behavioral signals to the ML service to
detect spam, random text and abuse. The gateway applies admission control
before calling the upstream and keeps the number of concurrent upstream calls
bounded.

## Run

```bash
cp gateway.example.yaml gateway.yaml
go run ./cmd/gateway -config gateway.yaml
```

`GET /healthz` is answered by the gateway itself and never proxied. For each
route, the gateway supports:

- exact patterns (`/v1/models`), segment parameters (`/v1/models/{id}`),
  one-segment wildcards (`/users/*`), recursive wildcards (`/users/**`) and
  regex (`re:^/v1/models/[a-z]+$`). The most specific route wins, scored per
  segment: literal > `{param}` > `*` > `**`. Regex routes score lowest, and on
  a tie the route declared first wins;
- a Gatekeeper with a per-route credential header (default `X-API-Key`) and a
  cap on concurrent calls per route. A route without `api_keys` is public; the
  gateway logs a warning for it at boot;
- a global `rate_limit` list, one route pattern per rule, keyed by `ip`,
  `api_key` or `ip_api_key`, using `token_bucket`, `fixed_window` or
  `sliding_window`. An unknown pattern or key fails at boot.

For the moderation route, send the model the text and the signals it expects,
for example `{"text":"...","event":"comment","account_age_days":2}`. The body
is forwarded unchanged, and the model's decision (for example `allow`,
`review` or `block`) and score are returned to the client. The gateway only
adds the trusted metadata headers `X-Gateway-Client-IP` and `X-Gateway-Route`,
replacing any copies sent by the client, so the model can correlate repeat
offenders without trusting spoofed headers.

### Admission and errors

`workers` caps concurrent upstream calls and `queue_size` caps how many more
requests may wait for a worker. A request arriving with both full is rejected
with `503`; a request whose client disconnects while waiting is dropped. Rate
limits and the per-route concurrency cap answer `429` (with `Retry-After` for
rate limits).

Upstream failures are logged and mapped to `413` (body above
`max_body_bytes`), `504` (route `timeout`, default 30s) or `502`. A route
timeout must stay below the server write timeout (60s); the gateway refuses to
start otherwise.

### Client IP behind a reverse proxy

By default the client IP is the TCP peer address. When the gateway runs behind
a reverse proxy (Traefik, a load balancer), list the proxy networks in
`trusted_proxies`:

```yaml
trusted_proxies: [10.0.0.0/8]
```

For requests coming from a trusted proxy, the client IP is the rightmost
`X-Forwarded-For` hop that is not itself a trusted proxy. Without this, every
client shares the proxy's IP, so `ip` rate limits become one global bucket.

### Token bucket and recursive routes

With `limit: 30`, `window: 1m` and `burst: 5`, each key (here, IP + credential)
starts with five tokens and gets one token back every two seconds on average.
Each call spends one token. Without tokens the call gets `429` with
`Retry-After`; no key sustains more than 30 calls per minute. The credential
used by the rate limit comes from the route's `gatekeeper.header`.

To apply independent policies to several route trees, use the global
`rate_limit` list. The most specific matching rule wins:

```yaml
- route: /users/** # /users/42 and /users/42/comments/7
  pattern: token_bucket
  limit: 30
  window: 1m
  burst: 5
  key: ip_api_key
- route: /users/me # wins over /users/**
  pattern: fixed_window
  limit: 10
  window: 1m
  key: ip_api_key
```

Proxy routes stay under `routes`; they define the upstream, methods and the
Gatekeeper. For example:

```yaml
routes:
  - name: users-moderation
    pattern: /users/**
    methods: [POST, PUT, PATCH]
    upstream: http://ml-moderation:8000
    gatekeeper:
      header: X-Client-Token
      api_keys: [replace-with-a-secret]
```

## Verification

```bash
go test ./...
go vet ./...
```

In production, inject the keys from a secrets manager when rendering the YAML;
never commit them. Limits are local to each instance. For a global limit across
replicas, replace the in-memory store with Redis or another atomic backplane.

## Container

```bash
docker build -t enois-gateway .
docker run --rm -p 8080:8080 -v "$PWD/gateway.yaml:/etc/enois/gateway.yaml:ro" enois-gateway
```

The image ships no configuration, so mounting one at
`/etc/enois/gateway.yaml` is required; without it the gateway fails at boot.
The `Dockerfile` uses a multi-stage build, a static stripped binary, a
`scratch` image, CA certificates for HTTPS upstreams and a non-root user. In
the YAML, `gatekeeper.header` picks where the key comes from; for example, use
`header: Authorization` if the client sends the token there (the value must
match an entry in `api_keys` exactly).
