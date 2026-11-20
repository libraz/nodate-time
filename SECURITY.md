# Security policy

## Reporting a vulnerability

Report it privately through GitHub: open the repository's **Security** tab and choose **Report a vulnerability**. Include a description, the affected version or commit, and steps to reproduce. Please do not open a public issue for a vulnerability. A partial report is fine; the maintainer fills in the rest.

## Supported versions

The project is pre-1.0 and has not been independently audited. Only the latest release receives fixes.

## Scope

In scope:

- Authentication and session handling: sign-in, password reset, OAuth and magic-link flows, token and cookie handling, rate limits.
- Authorization: reaching a calendar, event, memo, album or attachment without the membership or role that grants it, or reading another member's hidden fields (for example email addresses).
- Public share links and the embeddable view: exposing more than the share grants.
- Object storage access: signed URLs and blob download or upload outside the caller's calendar.
- Injection and content handling: SQL injection, script injection through event, memo or comment text or uploaded files, and calendar (iCal) import or export that crashes, hangs or grows without bound on crafted input.
- The administrative listener (metrics, profiles, health) being reachable by default from outside the host.

Out of scope:

- Findings that need an operator to have disabled a safeguard, such as running with `TC_ENV=development` in production or publishing the administrative listener.
- Missing hardening on a deployment the operator configured differently from `compose.prod.yml` (a host proxy, a CDN).
- Denial of service by sheer request volume against an instance without rate limits configured.
- Vulnerabilities in dependencies, MySQL, the object-storage gateway or the shared schema contract in [nodate-flow](https://github.com/libraz/nodate-flow); report them to those projects. A way this project uses one of them unsafely is in scope.
