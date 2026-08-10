import { createWorkOS, type User } from "@workos-inc/node";
import { app, dialog, ipcMain, safeStorage, shell } from "electron";
import {
  chmod,
  mkdir,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import path from "node:path";
import type { CloudAccount } from "../shared/cloud-account";

const CLIENT_ID =
  import.meta.env.VITE_WORKOS_CLIENT_ID?.trim() ||
  (process.env.VITEST ? "client_test" : "");
const REDIRECT_URI = "ao-app://callback";
const AUTH_STORE_FILE = "cloud-auth.bin";
const LEGACY_SESSION_FILE = "cloud-session.json";
const PKCE_TTL_MS = 10 * 60 * 1000;
const workos = CLIENT_ID ? createWorkOS({ clientId: CLIENT_ID }) : null;

interface StoredSession extends CloudAccount {
  accessToken: string;
  refreshToken: string;
}

interface AuthStore {
  session: StoredSession | null;
  pkce: {
    codeVerifier: string;
    state: string;
    expiresAt: number;
  } | null;
}

const emptyStore = (): AuthStore => ({ session: null, pkce: null });
const memoryStores = new Map<string, AuthStore>();
const refreshes = new Map<string, Promise<CloudAccount | null>>();

function storePath(dataDir: string): string {
  return path.join(dataDir, AUTH_STORE_FILE);
}

function encodeStore(store: AuthStore): Buffer {
  return safeStorage.encryptString(JSON.stringify(store));
}

function decodeStore(value: Buffer): AuthStore {
  return JSON.parse(safeStorage.decryptString(value)) as AuthStore;
}

async function readAuthStore(dataDir: string): Promise<AuthStore> {
  const memoryStore = memoryStores.get(dataDir);
  if (memoryStore) return memoryStore;
  if (!safeStorage.isEncryptionAvailable()) {
    await rm(storePath(dataDir), { force: true });
    return emptyStore();
  }
  try {
    return decodeStore(await readFile(storePath(dataDir)));
  } catch {
    await rm(storePath(dataDir), { force: true });
    return emptyStore();
  }
}

async function writeAuthStore(
  dataDir: string,
  store: AuthStore,
): Promise<void> {
  if (!safeStorage.isEncryptionAvailable()) {
    // A rotating refresh token must never fall back to plaintext persistence.
    // Keep the session process-local until the OS keyring becomes available.
    memoryStores.set(dataDir, store);
    await rm(storePath(dataDir), { force: true });
    return;
  }
  memoryStores.delete(dataDir);
  await mkdir(dataDir, { recursive: true });
  const target = storePath(dataDir);
  await writeFile(target, encodeStore(store), { mode: 0o600 });
  await chmod(target, 0o600);
}

async function removeAuthStore(dataDir: string): Promise<void> {
  memoryStores.delete(dataDir);
  await Promise.all([
    rm(storePath(dataDir), { force: true }),
    rm(path.join(dataDir, LEGACY_SESSION_FILE), { force: true }),
  ]);
}

function displayName(user: User): string {
  return (
    user.name?.trim() ||
    [user.firstName, user.lastName].filter(Boolean).join(" ") ||
    user.email
  );
}

function toStoredSession(
  accessToken: string,
  refreshToken: string,
  user: User,
): StoredSession {
  return {
    accessToken,
    refreshToken,
    authProvider: "workos",
    user: {
      id: user.id,
      email: user.email,
      displayName: displayName(user),
    },
    storedAt: new Date().toISOString(),
  };
}

function publicAccount(session: StoredSession): CloudAccount {
  return {
    authProvider: session.authProvider,
    user: session.user,
    storedAt: session.storedAt,
  };
}

function jwtPayload(token: string): Record<string, unknown> | null {
  try {
    const payload = token.split(".")[1];
    if (!payload) return null;
    return JSON.parse(
      Buffer.from(payload.replace(/-/g, "+").replace(/_/g, "/"), "base64").toString("utf8"),
    ) as Record<string, unknown>;
  } catch {
    return null;
  }
}

function tokenExpiresSoon(token: string): boolean {
  const exp = jwtPayload(token)?.exp;
  return typeof exp !== "number" || Date.now() >= exp * 1000 - 60_000;
}

export async function getCloudSession(
  dataDir: string,
): Promise<CloudAccount | null> {
  if (!workos) return null;
  const activeRefresh = refreshes.get(dataDir);
  if (activeRefresh) return activeRefresh;
  const store = await readAuthStore(dataDir);
  if (!store.session) return null;
  if (!tokenExpiresSoon(store.session.accessToken)) {
    return publicAccount(store.session);
  }

  const pendingRefresh = refreshes.get(dataDir);
  if (pendingRefresh) return pendingRefresh;
  const refresh = refreshCloudSession(dataDir, store, store.session);
  refreshes.set(dataDir, refresh);
  try {
    return await refresh;
  } finally {
    if (refreshes.get(dataDir) === refresh) refreshes.delete(dataDir);
  }
}

async function refreshCloudSession(
  dataDir: string,
  store: AuthStore,
  storedSession: StoredSession,
): Promise<CloudAccount | null> {
  try {
    const refreshed = await workos!.userManagement.authenticateWithRefreshToken({
      clientId: CLIENT_ID,
      refreshToken: storedSession.refreshToken,
    });
    const session = toStoredSession(
      refreshed.accessToken,
      refreshed.refreshToken,
      refreshed.user,
    );
    await writeAuthStore(dataDir, { ...store, session });
    return publicAccount(session);
  } catch {
    await removeAuthStore(dataDir);
    return null;
  }
}

export async function beginCloudSignIn(dataDir: string): Promise<void> {
  if (!workos) {
    throw new Error("WorkOS is not configured.");
  }
  const { url, state, codeVerifier } =
    await workos.userManagement.getAuthorizationUrlWithPKCE({
      clientId: CLIENT_ID,
      provider: "authkit",
      prompt: "login",
      maxAge: 0,
      redirectUri: REDIRECT_URI,
    });
  const store = await readAuthStore(dataDir);
  await writeAuthStore(dataDir, {
    ...store,
    pkce: {
      codeVerifier,
      state,
      expiresAt: Date.now() + PKCE_TTL_MS,
    },
  });
  await shell.openExternal(url);
}

export async function handleCloudDeepLink(
  rawURL: string,
  dataDir: string,
): Promise<CloudAccount | null> {
  if (!workos) throw new Error("WorkOS is not configured.");
  const url = new URL(rawURL);
  if (url.protocol !== "ao-app:" || url.hostname !== "callback") return null;
  const error = url.searchParams.get("error");
  if (error) {
    throw new Error(
      url.searchParams.get("error_description") || `WorkOS sign-in failed: ${error}`,
    );
  }
  const code = url.searchParams.get("code");
  const callbackState = url.searchParams.get("state");
  if (!code || !callbackState) throw new Error("WorkOS callback is incomplete.");

  const store = await readAuthStore(dataDir);
  if (!store.pkce) throw new Error("No WorkOS sign-in is pending.");
  if (store.pkce.expiresAt < Date.now()) {
    await writeAuthStore(dataDir, { ...store, pkce: null });
    throw new Error("The WorkOS sign-in request expired.");
  }
  if (callbackState !== store.pkce.state) {
    throw new Error("WorkOS callback state did not match.");
  }

  const result = await workos.userManagement.authenticateWithCode({
    clientId: CLIENT_ID,
    code,
    codeVerifier: store.pkce.codeVerifier,
  });
  const session = toStoredSession(
    result.accessToken,
    result.refreshToken,
    result.user,
  );
  await writeAuthStore(dataDir, { session, pkce: null });
  return publicAccount(session);
}

export async function signOutCloud(dataDir: string): Promise<void> {
  await removeAuthStore(dataDir);
}

export function registerCloudProtocol(): void {
  if (process.defaultApp && process.argv.length >= 2) {
    app.setAsDefaultProtocolClient("ao-app", process.execPath, [
      path.resolve(process.argv[1]),
    ]);
    return;
  }
  app.setAsDefaultProtocolClient("ao-app");
}

export function installCloudIPC(
  getDataDir: () => string,
  notifyRenderers: (session: CloudAccount | null) => void,
): void {
  ipcMain.handle("cloud:getSession", () => getCloudSession(getDataDir()));
  ipcMain.handle("cloud:signIn", async () => {
    if (!workos) {
      await dialog.showMessageBox({
        type: "info",
        title: "WorkOS not configured",
        message: "WorkOS sign-in is not configured.",
        detail:
          "Set VITE_WORKOS_CLIENT_ID and restart Agent Orchestrator to enable sign-in.",
      });
      return;
    }
    await beginCloudSignIn(getDataDir());
  });
  ipcMain.handle("cloud:signOut", async () => {
    await signOutCloud(getDataDir());
    notifyRenderers(null);
  });
}
