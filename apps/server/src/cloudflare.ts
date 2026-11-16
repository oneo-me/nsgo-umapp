// The Cloudflare accounts CF_API_TOKEN can see, for Umapp to add their websites with nothing to embed: a site of the server's own account (CF_ACCOUNT_ID) gets routes to the server, whose injector adds the tracker; a site of another account gets routes to the relay uploaded there (relay.ts), as a route leads only to a Worker of its zone's account.

import { listSites, normalizeDomain, parseHostname, siteState, sitePatterns, zoneOf } from "../scripts/sites.mjs";
import type { Env } from "./collect";
import { RELAY, relaySource } from "./relay";

const API = "https://api.cloudflare.com/client/v4";

/** CloudflareError is a refusal to show the app: Cloudflare's, or one of these functions'. */
export class CloudflareError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

interface Zone {
  id: string;
  name: string;
  accountId: string;
  accountName: string;
}

interface Route {
  id: string;
  pattern: string;
  script: string;
}

/** Site is a website of the accounts as the app shows it: added, can be added, or why it can't. */
export interface Site {
  domain: string;
  account: string;
  tracked: boolean;
  unavailable: string;
}

/** scriptName is the server's own Worker. */
function scriptName(env: Env): string {
  return env.SCRIPT_NAME || "umapp-server";
}

async function call<T>(env: Env, f: typeof fetch, method: string, path: string, body?: BodyInit, json?: unknown): Promise<{ result: T; result_info?: { page?: number; per_page?: number; total_count?: number } }> {
  const headers: Record<string, string> = { Authorization: `Bearer ${env.CF_API_TOKEN}` };
  if (json !== undefined) headers["Content-Type"] = "application/json";
  const res = await f(API + path, { method, headers, body: json !== undefined ? JSON.stringify(json) : body });
  const answer = (await res.json().catch(() => ({}))) as { success?: boolean; result?: T; errors?: { code?: number; message?: string }[]; result_info?: { page?: number; per_page?: number; total_count?: number } };
  if (!res.ok || answer.success === false) {
    const why = (answer.errors ?? []).map((e) => e.message).filter(Boolean).join("; ") || `HTTP ${res.status}`;
    const denied = res.status === 401 || res.status === 403;
    throw new CloudflareError(502, denied ? `Cloudflare refused the server's token: ${why}. It needs Zone Read, Workers Routes Edit and Workers Scripts Edit.` : `Cloudflare answered ${method} ${path}: ${why}`);
  }
  return { result: answer.result as T, result_info: answer.result_info };
}

/** zones are the active zones the token can see, in every account it covers. */
async function zones(env: Env, f: typeof fetch): Promise<Zone[]> {
  const out: Zone[] = [];
  for (let page = 1; ; page++) {
    const { result, result_info } = await call<{ id: string; name: string; status: string; account?: { id: string; name: string } }[]>(env, f, "GET", `/zones?status=active&per_page=50&page=${page}`);
    for (const z of result ?? []) {
      if (z.status === "active" && z.account?.id) out.push({ id: z.id, name: z.name.toLowerCase(), accountId: z.account.id, accountName: z.account.name });
    }
    const info = result_info ?? {};
    if (!result?.length || !info.total_count || (info.page ?? page) * (info.per_page ?? 50) >= info.total_count) break;
  }
  return out.sort((a, b) => a.name.localeCompare(b.name));
}

async function routes(env: Env, f: typeof fetch, list: Zone[]): Promise<Map<string, Route[]>> {
  const out = new Map<string, Route[]>();
  for (let i = 0; i < list.length; i += 10) {
    await Promise.all(
      list.slice(i, i + 10).map(async (z) => {
        const { result } = await call<{ id: string; pattern: string; script?: string }[]>(env, f, "GET", `/zones/${z.id}/workers/routes`);
        out.set(z.id, (result ?? []).map((r) => ({ id: r.id, pattern: r.pattern, script: r.script ?? "" })));
      }),
    );
  }
  return out;
}

/** scriptFor is the Worker a zone's routes lead to: the server in its own account, the relay elsewhere. */
function scriptFor(env: Env, zone: Zone): string {
  return zone.accountId === env.CF_ACCOUNT_ID ? scriptName(env) : RELAY;
}

/** websiteDomains maps every website's normalized domain to it, and whether the Worker tracks it. */
async function websiteDomains(env: Env): Promise<Map<string, { id: string; autoTrack: boolean }>> {
  const { results } = await env.DB.prepare("SELECT id, domain, auto_track FROM website").all<{ id: string; domain: string; auto_track: number }>();
  const all = new Map<string, { id: string; autoTrack: boolean }>();
  for (const r of results) all.set(normalizeDomain(r.domain), { id: r.id, autoTrack: r.auto_track === 1 });
  return all;
}

interface Accounts {
  zones: Zone[];
  routes: Map<string, Route[]>;
  websites: Map<string, { id: string; autoTrack: boolean }>;
}

async function load(env: Env, f: typeof fetch): Promise<Accounts> {
  if (!env.CF_API_TOKEN) throw new CloudflareError(501, "The server has no Cloudflare token: run npm run deploy to give it one.");
  const list = await zones(env, f);
  const [byZone, websites] = await Promise.all([routes(env, f, list), websiteDomains(env)]);
  return { zones: list, routes: byZone, websites };
}

/** listCloudflareSites is every site of the accounts: each zone's apex and the hostnames already routed to the Worker, tracked when the Worker has their routes and their website. ownHosts are the hostnames the server answers Umapp at, which are no site. */
export async function listCloudflareSites(env: Env, f: typeof fetch, ownHosts: string[]): Promise<Site[]> {
  const a = await load(env, f);
  const tracked = new Set(a.websites.keys());
  const out: Site[] = [];
  for (const zone of a.zones) {
    for (const s of listSites([zone], a.routes, scriptFor(env, zone), ownHosts, tracked)) {
      out.push({ domain: s.domain, account: zone.accountName, tracked: s.tracked, unavailable: s.unavailable });
    }
  }
  return out;
}

/** uploadRelay puts the relay in account, tracking the websites there that the Worker tracks and those of extra, with the server at origin. */
async function uploadRelay(env: Env, f: typeof fetch, a: Accounts, account: string, origin: string, extra: Map<string, string>) {
  const sites: Record<string, string> = {};
  for (const [domain, w] of a.websites) {
    const zone = zoneOf(domain, a.zones);
    if (zone?.accountId === account && w.autoTrack) sites[domain] = w.id;
  }
  for (const [domain, id] of extra) sites[domain] = id;
  const form = new FormData();
  form.set(
    "metadata",
    new Blob([JSON.stringify({ main_module: "relay.js", compatibility_date: "2026-10-01", bindings: [
      { type: "plain_text", name: "SITES", text: JSON.stringify(sites) },
      { type: "plain_text", name: "SERVER", text: origin },
    ] })], { type: "application/json" }),
  );
  form.set("relay.js", new Blob([relaySource()], { type: "application/javascript+module" }), "relay.js");
  await call(env, f, "PUT", `/accounts/${account}/workers/scripts/${RELAY}`, form);
}

/** addCloudflareSite tracks the site of domain in the accounts: its website, made when missing, then the relay in its account when that is another, then its routes. origin is the server's own, which the relay's pages send their events to. */
export async function addCloudflareSite(env: Env, f: typeof fetch, input: string, origin: string, ownHosts: string[], now: number): Promise<string> {
  const host = parseHostname(input);
  if (!host) throw new CloudflareError(400, "That isn't a hostname.");
  const domain = normalizeDomain(host);
  const a = await load(env, f);
  const zone = zoneOf(domain, a.zones) as Zone | undefined;
  if (!zone) throw new CloudflareError(404, `${domain} is in none of the Cloudflare accounts the server's token can see.`);
  const script = scriptFor(env, zone);
  const state = siteState(domain, zone, a.routes.get(zone.id) ?? [], script, ownHosts, new Set(a.websites.keys()));
  if (state.unavailable) throw new CloudflareError(409, `${domain} can't be tracked: ${state.unavailable}.`);

  let id = a.websites.get(domain)?.id;
  if (!id) {
    id = crypto.randomUUID();
    await env.DB.prepare("INSERT INTO website (id, name, domain, created_at, auto_track) VALUES (?, ?, ?, ?, 0)").bind(id, domain, domain, now).run();
  }
  if (script === RELAY) await uploadRelay(env, f, a, zone.accountId, origin, new Map([[domain, id]]));
  for (const p of state.patterns as { pattern: string; route?: Route; conflict?: Route }[]) {
    if (p.route || p.conflict) continue;
    await call(env, f, "POST", `/zones/${zone.id}/workers/routes`, undefined, { pattern: p.pattern, script });
  }
  await env.DB.prepare("UPDATE website SET auto_track = 1 WHERE id = ?").bind(id).run();
  return id;
}

/** removeCloudflareSite takes away the routes the server or the relay has for domain's site, keeping every other Worker's, and in another account uploads the relay again without it, so a route left behind adds no tracker for a website that is gone. origin is the server's own, which the relay keeps sending the account's other sites' events to. */
export async function removeCloudflareSite(env: Env, f: typeof fetch, domain: string, origin: string): Promise<void> {
  if (!env.CF_API_TOKEN) return;
  const list = await zones(env, f);
  const d = normalizeDomain(domain);
  const zone = zoneOf(d, list) as Zone | undefined;
  if (!zone) return;
  const script = scriptFor(env, zone);
  const patterns = new Set<string>(sitePatterns(d, zone.name));
  const { result } = await call<{ id: string; pattern: string; script?: string }[]>(env, f, "GET", `/zones/${zone.id}/workers/routes`);
  for (const r of result ?? []) {
    if (r.script === script && patterns.has(r.pattern.trim().toLowerCase())) await call(env, f, "DELETE", `/zones/${zone.id}/workers/routes/${r.id}`);
  }
  if (script === RELAY) {
    const websites = await websiteDomains(env);
    websites.delete(d);
    await uploadRelay(env, f, { zones: list, routes: new Map(), websites }, zone.accountId, origin, new Map());
  }
}

