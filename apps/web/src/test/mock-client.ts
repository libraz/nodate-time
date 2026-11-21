import { vi } from 'vitest';

type Route = Response | ((request: Request) => Response | Promise<Response>);

/** A JSON response with the given status. */
export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

/**
 * Replaces the global fetch with a router keyed by `"METHOD /path"` (query
 * string ignored). A route is a Response, used once per call via clone, or a
 * function of the request. An unrouted request fails the test. Returns the
 * mock so tests can inspect the requests the client sent.
 */
export function mockApi(routes: Record<string, Route>) {
  const fetchMock = vi.fn(async (input: Request | string, init?: RequestInit) => {
    const request = typeof input === 'string' ? new Request(input, init) : input;
    const key = `${request.method} ${new URL(request.url).pathname}`;
    const route = routes[key];
    if (!route) throw new Error(`mockApi: no route for ${key}`);
    return typeof route === 'function' ? route(request) : route.clone();
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}
