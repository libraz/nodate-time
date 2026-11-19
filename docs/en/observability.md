# Observability

The server serves two ports. `TC_PORT` (8080) is the product. `TC_METRICS_ADDR` (127.0.0.1:9464) is a second listener carrying what describes the process rather than the product:

| Path             | What it answers                                                                                                                                                                                                                    |
| ---------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/metrics`       | Prometheus text format: request rate, latency histogram and response size by matched route; requests in flight; connection-pool saturation; Go runtime; rate-limit rejections by scope; outgoing mail by outcome                   |
| `/debug/pprof/*` | live CPU, heap and goroutine profiles                                                                                                                                                                                              |
| `/health/detail` | per-dependency report (database, object storage, mail relay) with the error each failing probe returned. 503 only when a _required_ dependency is down, so a deployment is not pulled from rotation over an unreachable mail relay |

## Access control

The listener is guarded twice, and neither guard reads a forwarded header: it binds to loopback by default, and every request is matched against `TC_METRICS_ALLOWED_CIDRS` by its direct TCP peer. A request from outside the list gets 404 rather than 403, because a refusal that says "not allowed" confirms the endpoint exists.

**Do not put a reverse-proxy rule in front of this port.** Proxied, every request arrives from the proxy's address, so one allow-list entry would admit the whole internet.

`compose.prod.yml` publishes it as `127.0.0.1:9464` on the host; a scraper sharing the compose network reaches it at `api:9464`.

```sh
curl -s 127.0.0.1:9464/metrics | head
curl -s 127.0.0.1:9464/health/detail | jq
go tool pprof http://127.0.0.1:9464/debug/pprof/profile?seconds=30
```

The public `/health` stays what a load balancer needs: cheap, unauthenticated, and silent about which dependency is unwell.

## Access log

The access log names the route, never the URL. A share token is a path segment and a password reset carries its token in the query, so a conventional access log would file working credentials in the artifact that is kept longest and read most widely.

Each line carries the matched route pattern, status, duration, response size, client IP and a request identifier. The identifier is also returned in `X-Request-Id` and attached to every other line the request logs, so one value collects the whole story. An identifier supplied by the caller is adopted only from a hop listed in `TC_TRUSTED_PROXIES`.

The web service's nginx follows the same rule: its access log carries the path without the query string, with the token segment of `/share/`, `/embed/` and `/invites/` URLs replaced, and never the `Referer`. Its error log, written only for failures, quotes the request line and the `Referer` of the request that failed.
