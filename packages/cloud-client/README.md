# `@aoagents/cloud-client`

Runtime-neutral TypeScript contracts and a small fetch-based client for AO
Cloud's public API. The package defines the client boundary; this repository
does not implement the Cloud routes.

```ts
import { createCloudClient } from "@aoagents/cloud-client";

const cloud = createCloudClient({
  baseUrl: "https://cloud.example.com",
  getAccessToken: () => authSession.getAccessToken(),
  fetch,
});

const sessions = await cloud.listSessions(orgId, { limit: 50 });
```

The caller owns authentication and token refresh. This package only asks for an
access token immediately before a request; it never accepts or stores refresh
tokens.

The source contract is `contracts/cloud/openapi.yaml`. Run `npm run generate`
from this directory after changing it. The generated `src/schema.ts` file is
committed so consumers do not need an OpenAPI toolchain.

This boundary intentionally excludes sign-in flows, worker bootstrap and
commands, provisioning, database details, provider secrets, and local daemon
routes.
