import { createExecutionContext, env } from "cloudflare:test";
import { beforeEach, describe, expect, it } from "vitest";
import { api } from "../src/api";
import { RELAY, relayFetch, relaySource } from "../src/relay";

const KEY = { Authorization: "Bearer test-key" };
const OWN = "acc-own";
const OTHER = "acc-other";

interface FakeRoute {
  id: string;
  pattern: string;
  script: string;
}

/** fakeCloudflare answers the API calls the server makes, over two accounts: the server's own with example.com and taken.com, and another with other.app. */
function fakeCloudflare() {
  const zones = [
    { id: "z1", name: "example.com", status: "active", account: { id: OWN, name: "Own" } },
    { id: "z2", name: "taken.com", status: "active", account: { id: OWN, name: "Own" } },
    { id: "z3", name: "other.app", status: "active", account: { id: OTHER, name: "Other" } },
  ];
  const routes = new Map<string, FakeRoute[]>([
    ["z1", []],
    ["z2", [{ id: "t1", pattern: "taken.com/*", script: "someone-else" }]],
    ["z3", []],
  ]);
  const scripts = new Map<string, { metadata: any; source: string }>();
  let next = 0;
  const calls: string[] = [];
  const f = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input));
    const path = url.pathname.replace("/client/v4", "");
    const method = init?.method ?? "GET";
    calls.push(`${method} ${path}`);
    const ok = (result: unknown, extra: object = {}) => Response.json({ success: true, result, ...extra });
    if (method === "GET" && path === "/zones") return ok(zones, { result_info: { page: 1, per_page: 50, total_count: zones.length } });
    let m = path.match(/^\/zones\/(\w+)\/workers\/routes(?:\/(\w+))?$/);
    if (m) {
      const list = routes.get(m[1])!;
      if (method === "GET") return ok(list);
      if (method === "POST") {
        const body = JSON.parse(String(init!.body));
        const r = { id: `r${next++}`, pattern: body.pattern, script: body.script };
        list.push(r);
        return ok(r);
      }
      if (method === "DELETE") {
        routes.set(m[1], list.filter((r) => r.id !== m![2]));
        return ok({ id: m[2] });
      }
    }
    m = path.match(/^\/accounts\/([\w-]+)\/workers\/scripts\/([\w-]+)$/);
    if (m && method === "PUT") {
      const form = init!.body as FormData;
      const metadata = JSON.parse(await (form.get("metadata") as Blob).text());
      scripts.set(`${m[1]}/${m[2]}`, { metadata, source: await (form.get("relay.js") as Blob).text() });
      return ok({ id: m[2] });
    }
    return Response.json({ success: false, errors: [{ message: `no fake for ${method} ${path}` }] }, { status: 404 });
  }) as typeof fetch;
  return { f, routes, scripts, calls };
}

const cfEnv = { ...env, CF_API_TOKEN: "cf-token", CF_ACCOUNT_ID: OWN, SCRIPT_NAME: "umapp-server" };

async function call(method: string, path: string, f: typeof fetch, body?: unknown, e: typeof env = cfEnv) {
  const request = new Request("https://umapp-server.someone.workers.dev/api" + path, {
    method,
    headers: KEY,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const res = await api(request, e, path, Date.UTC(2026, 10, 16), f);
  return { status: res.status, body: (await res.json()) as any };
}

beforeEach(async () => {
  await env.DB.batch([env.DB.prepare("DELETE FROM event"), env.DB.prepare("DELETE FROM website")]);
});

describe("the Cloudflare accounts' sites", () => {
  it("lists every account's zones, the ones another Worker holds as unavailable", async () => {
    const cf = fakeCloudflare();
    const { status, body } = await call("GET", "/cloudflare", cf.f);
    expect(status).toBe(200);
    expect(body.sites).toEqual([
      { domain: "example.com", account: "Own", tracked: false, unavailable: "" },
      { domain: "other.app", account: "Other", tracked: false, unavailable: "" },
      { domain: "taken.com", account: "Own", tracked: false, unavailable: 'taken.com/* goes to the Worker "someone-else"' },
    ]);
  });

  it("adds a site of the server's own account with routes to the server", async () => {
    const cf = fakeCloudflare();
    const added = await call("POST", "/cloudflare/sites", cf.f, { domain: "https://www.Example.com/x" });
    expect(added.status).toBe(200);
    expect(added.body).toMatchObject({ domain: "example.com", autoTrack: true });
    expect(cf.routes.get("z1")!.map((r) => [r.pattern, r.script])).toEqual([
      ["example.com/*", "umapp-server"],
      ["www.example.com/*", "umapp-server"],
    ]);
    expect(cf.scripts.size).toBe(0);
    const list = await call("GET", "/cloudflare", cf.f);
    expect(list.body.sites[0]).toMatchObject({ domain: "example.com", tracked: true });
    // Adding it again changes nothing.
    await call("POST", "/cloudflare/sites", cf.f, { domain: "example.com" });
    expect(cf.routes.get("z1")).toHaveLength(2);
    expect((await call("GET", "/websites", cf.f)).body.count).toBe(1);
  });

  it("adds a site of another account through the relay uploaded there", async () => {
    const cf = fakeCloudflare();
    const added = await call("POST", "/cloudflare/sites", cf.f, { domain: "other.app" });
    expect(added.body).toMatchObject({ domain: "other.app", autoTrack: true });
    const relay = cf.scripts.get(`${OTHER}/${RELAY}`)!;
    const bindings = Object.fromEntries(relay.metadata.bindings.map((b: any) => [b.name, b.text]));
    expect(JSON.parse(bindings.SITES)).toEqual({ "other.app": added.body.id });
    expect(bindings.SERVER).toBe("https://umapp-server.someone.workers.dev");
    expect(relay.source).toContain("export default { fetch: ");
    expect(cf.routes.get("z3")!.map((r) => [r.pattern, r.script])).toEqual([
      ["other.app/*", RELAY],
      ["www.other.app/*", RELAY],
    ]);
  });

  it("keeps a website added by hand and adds it with nothing to embed", async () => {
    const cf = fakeCloudflare();
    const manual = await call("POST", "/websites", cf.f, { name: "Mine", domain: "example.com" });
    const added = await call("POST", "/cloudflare/sites", cf.f, { domain: "example.com" });
    expect(added.body).toMatchObject({ id: manual.body.id, name: "Mine", autoTrack: true });
  });

  it("refuses a site another Worker holds, one in no account, and works only with a token", async () => {
    const cf = fakeCloudflare();
    expect((await call("POST", "/cloudflare/sites", cf.f, { domain: "taken.com" })).status).toBe(409);
    expect((await call("POST", "/cloudflare/sites", cf.f, { domain: "nowhere.org" })).status).toBe(404);
    expect((await call("POST", "/cloudflare/sites", cf.f, { domain: "not a host" })).status).toBe(400);
    const none = await call("GET", "/cloudflare", cf.f, undefined, { ...env, CF_API_TOKEN: undefined } as typeof env);
    expect(none.status).toBe(501);
    expect(cf.routes.get("z2")!.map((r) => r.script)).toEqual(["someone-else"]);
  });

  it("takes a deleted website's routes away, and no other Worker's", async () => {
    const cf = fakeCloudflare();
    const added = await call("POST", "/cloudflare/sites", cf.f, { domain: "example.com" });
    cf.routes.get("z1")!.push({ id: "keep", pattern: "example.com/api/*", script: "someone-else" });
    const deleted = await call("DELETE", `/websites/${added.body.id}`, cf.f);
    expect(deleted.body).toEqual({ ok: true });
    expect(cf.routes.get("z1")!.map((r) => r.id)).toEqual(["keep"]);
  });

  it("takes a deleted website of another account out of the relay there, keeping the account's others", async () => {
    const cf = fakeCloudflare();
    cf.routes.set("z4", []);
    const zonesFake = cf.f;
    const f = (async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).includes("/zones?")) {
        const res = await zonesFake(input, init);
        const body = (await res.json()) as any;
        body.result.push({ id: "z4", name: "second.app", status: "active", account: { id: OTHER, name: "Other" } });
        body.result_info.total_count = body.result.length;
        return Response.json(body);
      }
      return zonesFake(input, init);
    }) as typeof fetch;
    const first = await call("POST", "/cloudflare/sites", f, { domain: "other.app" });
    const second = await call("POST", "/cloudflare/sites", f, { domain: "second.app" });
    const sites = () => JSON.parse(cf.scripts.get(`${OTHER}/${RELAY}`)!.metadata.bindings.find((b: any) => b.name === "SITES").text);
    expect(sites()).toEqual({ "other.app": first.body.id, "second.app": second.body.id });
    expect((await call("DELETE", `/websites/${first.body.id}`, f)).body).toEqual({ ok: true });
    expect(cf.routes.get("z3")).toEqual([]);
    expect(sites()).toEqual({ "second.app": second.body.id });
  });

  it("deletes the website even when Cloudflare refuses, saying why", async () => {
    const cf = fakeCloudflare();
    const added = await call("POST", "/cloudflare/sites", cf.f, { domain: "example.com" });
    const refusing = (async () => Response.json({ success: false, errors: [{ message: "Authentication error" }] }, { status: 403 })) as typeof fetch;
    const deleted = await call("DELETE", `/websites/${added.body.id}`, refusing);
    expect(deleted.body.ok).toBe(true);
    expect(deleted.body.warning).toMatch(/refused the server's token/);
    expect((await call("GET", `/websites/${added.body.id}`, cf.f)).status).toBe(404);
  });
});

describe("the relay", () => {
  const relayEnv = { SITES: JSON.stringify({ "other.app": "site-1" }), SERVER: "https://umapp-server.someone.workers.dev/" };
  const page = (headers: Record<string, string> = { "Content-Type": "text/html; charset=utf-8" }) =>
    (async () => new Response("<html><head><title>x</title></head><body>hi</body></html>", { headers })) as typeof fetch;

  it("adds the server's tracker to a page of a site it tracks", async () => {
    const res = await relayFetch(new Request("https://www.other.app/"), relayEnv, createExecutionContext(), page());
    const html = await res.text();
    expect(html).toContain('<script defer src="https://umapp-server.someone.workers.dev/script.js" data-website-id="site-1" data-host-url="https://umapp-server.someone.workers.dev"></script></head>');
  });

  it("passes anything else through", async () => {
    const ctx = createExecutionContext();
    for (const [url, init, origin] of [
      ["https://unknown.app/", {}, page()],
      ["https://other.app/", { method: "POST", body: "x" }, page()],
      ["https://other.app/a.css", {}, page({ "Content-Type": "text/css" })],
    ] as const) {
      const res = await relayFetch(new Request(url, init), relayEnv, ctx, origin);
      expect(await res.text()).not.toContain("script.js");
    }
  });

  it("uploads as a module of its own", () => {
    expect(relaySource()).toContain("HTMLRewriter");
    expect(relaySource()).not.toMatch(/\bimport\b/);
    // The bundle Wrangler deploys wraps inner functions in __name, which the module must define.
    expect(relaySource()).toMatch(/^const __name = /);
  });
});
