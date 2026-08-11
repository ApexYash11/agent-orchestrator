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
git -c submodule.private/ao-cloud.update=checkout \
  submodule update --init private/ao-cloud
```

Developers without access do nothing. A normal clone, build, and test of public
AO—including `git clone --recursive`—continues to work because the submodule is
configured with `update = none`. Only the explicit opt-in command above fails
without private repository permission.

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
- Organization-scoped account, project, session-policy, event, and GitHub
  OpenAPI contracts with generated TypeScript schema types.
- A typed Cloud client for bearer authentication, pagination, idempotent writes,
  cursor-safe reconnecting SSE, GitHub App flows, terminal tickets, and
  workspace reads.
- Reusable board, composer, inspector, project-settings, agent, and SCM
  presentation used by the local desktop app.
- WorkOS desktop authentication with token custody in Electron main and a
  token-free renderer account projection.

These are shared-ready boundaries, not a hosted Cloud implementation.

## Private implementation status

The private repository now contains:

- the 28-table PostgreSQL schema, tenant keys, forced RLS, organizations,
  memberships, projects, sessions, turns/events, and future execution,
  sharing, and GitHub records;
- WorkOS access-token validation, organization authorization, idempotent
  project/session/message APIs, durable workspace intent, and cross-replica
  event replay/SSE;
- secure GitHub App installation, OAuth verification, repository grants,
  synchronization, disconnect, project import, and durable webhook processing;
- non-root control-plane and migration images;
- separate staging and production RDS/ECS/ALB/secrets/logging environments; and
- migration-first staging deployment plus exact-digest production promotion
  with scanning, health checks, automatic rollback, guarded manual rollback,
  CloudWatch alarms, and an operations dashboard.

The public submodule pointer records the private `main` commit known to be
compatible with this public branch. It is a development reference only; public
builds and releases still do not initialize or package the private repository.

## Environment modes

1. **Local:** `npm run cloud:local` uses local auth, local PostgreSQL, and the
   local control-plane container. It does not load WorkOS or GitHub App
   credentials. Docker workers are intended but are not implemented yet.
2. **Staging:** `npm run cloud:staging` runs the desktop locally against
   `https://staging-api.aoagents.dev`, the hosted staging database, and the
   shared WorkOS environment. Future staging workers run remotely.
3. **Production:** `https://api.aoagents.dev` uses the production database, the
   same WorkOS environment, and the production-owned GitHub App. There is no
   local-desktop-against-production development command.

GitHub App credentials remain disabled outside production. A future broker must
return signed, environment-scoped repository grants before local or staging UI
enables GitHub; sharing credentials directly would route callback state to the
wrong database.

## Private implementation still required

1. **Execution plane:** queues, leases, reconciliation, provisioning, sandbox
   images, workers, heartbeats, terminal transport, and workspace RPC.
2. **Cloud app:** organization selection plus project, session, chat, files,
   terminal, review, and settings screens backed by the generated client and
   shared product UI.
3. **SCM completion:** production GitHub App credentials, an environment broker,
   personal GitHub OAuth, scoped installation-token brokering for workers,
   PR/issue/check/review synchronization, and stale-head guards.
4. **Operations:** retire the empty internal ALBs after observation, move tasks
   to private subnets, configure SNS alarm notifications, and complete billing,
   backup restore drills, incident controls, and compatibility policy.

## Recommended implementation order

1. Build the first Cloud app flows with the generated client and shared UI.
2. Add provisioning and the worker protocol for real hosted sessions.
3. Add the production GitHub broker, worker token brokering, and SCM
   synchronization.
4. Add terminal/files, sharing, review synchronization, and remaining operations
   hardening.

See [cloud-refactor.md](cloud-refactor.md) for package ownership, import rules,
and the detailed public/private boundary.
