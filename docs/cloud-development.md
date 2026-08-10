# Cloud development

## TL;DR

AO stays a complete public local product. The private `ao-cloud` repository
implements the hosted control plane and can be checked out at
`private/ao-cloud` as an optional submodule for developers who have access.
Public builds, tests, and contributors must not depend on that checkout.

The public repository owns stable contracts, generated client types, reusable
product UI, and local desktop behavior. The private repository owns tenant data,
authorization, hosted execution, infrastructure, and secrets.

## Using the private checkout

After the optional submodule is added, an authorized developer initializes it
with:

```bash
gh auth setup-git
git submodule update --init private/ao-cloud
```

Developers without access do nothing. A normal clone, build, and test of public
AO continues to work; only submodule initialization fails without private
repository permission.

Work in the two repositories remains separate:

1. Commit and push private implementation changes from `private/ao-cloud`.
2. In the public repository, stage the `private/ao-cloud` path to record the
   known-compatible private commit.
3. Review the private code and the public submodule-pointer update in separate
   pull requests.

Never put credentials in `.gitmodules`. Public and fork CI must not initialize
the private submodule. A future integration job should use a scoped GitHub App
token with read access to `ao-cloud`.

## Foundation implemented in public AO

- Stable Go facts and pure rules for agents, sessions, status, PRs, reviews, and
  stack position.
- An organization-scoped Cloud OpenAPI contract and generated TypeScript schema
  types.
- A typed Cloud client for bearer authentication, pagination, idempotent writes,
  event replay/SSE, terminal tickets, and workspace reads.
- Reusable board, composer, inspector, project-settings, agent, and SCM
  presentation used by the local desktop app.
- WorkOS desktop authentication with token custody in Electron main and a
  token-free renderer account projection.

These are shared-ready boundaries, not a hosted Cloud implementation.

## Private implementation still required

1. **Data model:** PostgreSQL tables, migrations, tenant keys, RLS, organizations,
   memberships, projects, sessions, turns/events, provider connections, reviews,
   and execution records.
2. **Control plane:** hosted handlers for the public OpenAPI contract, WorkOS
   access-token validation, organization authorization, idempotency, and durable
   event replay.
3. **Execution plane:** queues, leases, reconciliation, provisioning, sandbox
   images, workers, heartbeats, terminal transport, and workspace RPC.
4. **Cloud app:** organization selection plus project, session, chat, files,
   terminal, review, and settings screens backed by the generated client and
   shared product UI.
5. **GitHub App:** installation flow, scoped token brokering, webhooks,
   repository access, PR/check/review synchronization, and stale-head guards.
6. **Operations:** deployment, observability, billing, backups, incident
   controls, and public/private release compatibility.

## Recommended implementation order

1. Define the private PostgreSQL schema and tenant/authorization model.
2. Implement the existing OpenAPI handlers and contract tests.
3. Build the first Cloud app flows with the generated client and shared UI.
4. Add provisioning and the worker protocol for real hosted sessions.
5. Add GitHub App installation, repository access, and SCM synchronization.
6. Harden retries, reconciliation, security, observability, billing, and
   release/version policy.

See [cloud-refactor.md](cloud-refactor.md) for package ownership, import rules,
and the detailed public/private boundary.
