// The part of Umami's API Umapp reads, answered in Umami 3's form, and the websites it manages.

import { addCloudflareSite, CloudflareError, listCloudflareSites, removeCloudflareSite } from "./cloudflare";
import { type Env, forget } from "./collect";
import { buckets, type Unit, validZone } from "./zone";

const LIVE_WINDOW = 5 * 60_000;
const MAX_RANGE = 400 * 86_400_000;
const MAX_BUCKETS = 500;

class HTTPError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

/** SERVER_HEADER tells Umapp it talks to its own server, which has no dashboard of Umami's to open websites in. */
export const SERVER_HEADER = "X-Umapp-Server";

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", "Cache-Control": "no-store", [SERVER_HEADER]: "1" },
  });
}

/** authorized compares the key a request carries, as Umami's bearer token or Umami Cloud's header, with API_KEY in constant time. */
async function authorized(request: Request, key: string): Promise<boolean> {
  const auth = request.headers.get("Authorization") ?? "";
  const given = auth.startsWith("Bearer ") ? auth.slice(7) : (request.headers.get("x-umami-api-key") ?? "");
  const enc = new TextEncoder();
  const [a, b] = await Promise.all([
    crypto.subtle.digest("SHA-256", enc.encode(given)),
    crypto.subtle.digest("SHA-256", enc.encode(key)),
  ]);
  return crypto.subtle.timingSafeEqual(a, b);
}

interface Range {
  from: number;
  to: number;
  tag: string;
}

function range(q: URLSearchParams): Range {
  const from = Number(q.get("startAt"));
  const to = Number(q.get("endAt"));
  if (!Number.isSafeInteger(from) || !Number.isSafeInteger(to) || from < 0 || to < from) {
    throw new HTTPError(400, "startAt and endAt must be times in milliseconds, startAt first.");
  }
  if (to - from > MAX_RANGE) throw new HTTPError(400, "The range is longer than 400 days.");
  return { from, to, tag: q.get("tag") ?? "" };
}

/** where is the condition and bindings of a website's events in r, after the website's id as ?1, on the columns of table t. */
function where(r: Range, t = "event"): [string, unknown[]] {
  const cond = `${t}.website_id = ?1 AND ${t}.created_at BETWEEN ?2 AND ?3`;
  return r.tag ? [`${cond} AND ${t}.tag = ?4`, [r.from, r.to, r.tag]] : [cond, [r.from, r.to]];
}

interface Stats {
  pageviews: number;
  visitors: number;
  visits: number;
  bounces: number;
  totaltime: number;
}

async function totals(env: Env, id: string, r: Range): Promise<Stats> {
  const [cond, args] = where(r);
  const row = await env.DB.prepare(
    `WITH v AS (
       SELECT MIN(session_id) AS session, COUNT(*) AS events, SUM(type = 1) AS views, MAX(created_at) - MIN(created_at) AS span
       FROM event WHERE ${cond} GROUP BY visit_id)
     SELECT COALESCE(SUM(views), 0) AS pageviews, COUNT(DISTINCT session) AS visitors, COUNT(*) AS visits,
       COALESCE(SUM(events = 1), 0) AS bounces, COALESCE(SUM(span), 0) / 1000 AS totaltime FROM v`,
  )
    .bind(id, ...args)
    .first<Stats>();
  return row ?? { pageviews: 0, visitors: 0, visits: 0, bounces: 0, totaltime: 0 };
}

async function stats(env: Env, id: string, q: URLSearchParams) {
  const r = range(q);
  const len = r.to - r.from;
  const [cur, prev] = await Promise.all([totals(env, id, r), totals(env, id, { ...r, from: r.from - len - 1, to: r.from - 1 })]);
  return { ...cur, comparison: prev };
}

async function active(env: Env, id: string, now: number) {
  const row = await env.DB.prepare("SELECT COUNT(DISTINCT session_id) AS visitors FROM event WHERE website_id = ? AND created_at >= ?")
    .bind(id, now - LIVE_WINDOW)
    .first<{ visitors: number }>();
  return { visitors: row?.visitors ?? 0 };
}

async function pageviews(env: Env, id: string, q: URLSearchParams) {
  const r = range(q);
  const unit = q.get("unit") ?? "day";
  if (unit !== "hour" && unit !== "day") throw new HTTPError(400, "unit must be hour or day.");
  const tz = q.get("timezone") || "UTC";
  if (!validZone(tz)) throw new HTTPError(400, "Unknown time zone.");
  const bs = buckets(r.from, r.to, unit as Unit, tz, MAX_BUCKETS);
  if (!bs) throw new HTTPError(400, `The range has more than ${MAX_BUCKETS} units.`);
  if (bs.length === 0) return { pageviews: [], sessions: [] };
  // The buckets are numbers computed here, written into the query as its table of ranges.
  const values = bs.map((b, i) => `(${i}, ${b.start}, ${b.end})`).join(", ");
  const [cond, args] = where(r, "e");
  const { results } = await env.DB.prepare(
    `WITH b (i, a, z) AS (VALUES ${values})
     SELECT b.i AS i, SUM(e.type = 1) AS views, COUNT(DISTINCT e.session_id) AS visitors
     FROM b JOIN event e ON e.created_at >= b.a AND e.created_at < b.z AND ${cond}
     GROUP BY b.i ORDER BY b.i`,
  )
    .bind(id, ...args)
    .all<{ i: number; views: number; visitors: number }>();
  return {
    pageviews: results.filter((row) => row.views > 0).map((row) => ({ x: bs[row.i].label, y: row.views })),
    sessions: results.map((row) => ({ x: bs[row.i].label, y: row.visitors })),
  };
}

// What each breakdown counts: pageviews of a page, visitors of the rest.
const breakdowns: Record<string, { column: string; count: string }> = {
  path: { column: "path", count: "SUM(type = 1)" },
  url: { column: "path", count: "SUM(type = 1)" },
  referrer: { column: "referrer", count: "COUNT(DISTINCT session_id)" },
  country: { column: "country", count: "COUNT(DISTINCT session_id)" },
  tag: { column: "tag", count: "COUNT(DISTINCT session_id)" },
  event: { column: "name", count: "COUNT(*)" },
};

async function metrics(env: Env, id: string, q: URLSearchParams) {
  const r = range(q);
  const b = breakdowns[q.get("type") ?? ""];
  if (!b) throw new HTTPError(400, "type must be path, referrer, country, tag or event.");
  const limit = Math.min(Math.max(Number(q.get("limit")) || 500, 1), 500);
  const [cond, args] = where(r);
  const { results } = await env.DB.prepare(
    `SELECT ${b.column} AS x, ${b.count} AS y FROM event WHERE ${cond} AND ${b.column} != ''
     GROUP BY ${b.column} HAVING y > 0 ORDER BY y DESC, x LIMIT ${limit}`,
  )
    .bind(id, ...args)
    .all<{ x: string; y: number }>();
  return results;
}

/** Website is a website in Umami's form, with autoTrack beside it: whether the Worker tracks it through its Cloudflare routes, so it needs no tracker embedded. */
interface Website {
  id: string;
  name: string;
  domain: string;
  createdAt: string;
  autoTrack: boolean;
}

type WebsiteRow = Omit<Website, "autoTrack"> & { autoTrack: number };

const websiteColumns = "id, name, domain, strftime('%Y-%m-%dT%H:%M:%fZ', created_at / 1000.0, 'unixepoch') AS createdAt, auto_track AS autoTrack";

function fromRow(row: WebsiteRow): Website {
  return { ...row, autoTrack: row.autoTrack === 1 };
}

async function website(env: Env, id: string): Promise<Website> {
  const row = await env.DB.prepare(`SELECT ${websiteColumns} FROM website WHERE id = ?`).bind(id).first<WebsiteRow>();
  if (!row) throw new HTTPError(404, "No such website.");
  return fromRow(row);
}

async function websites(env: Env) {
  const { results } = await env.DB.prepare(`SELECT ${websiteColumns} FROM website ORDER BY name, id`).all<WebsiteRow>();
  return { data: results.map(fromRow), count: results.length, page: 1, pageSize: results.length };
}

/** domainOf is the host of what the user typed, an address or a domain. */
function domainOf(s: string): string {
  const t = s.trim();
  try {
    return new URL(t.includes("://") ? t : "https://" + t).hostname;
  } catch {
    return "";
  }
}

async function createWebsite(env: Env, request: Request, now: number): Promise<Website> {
  let body: { name?: unknown; domain?: unknown };
  try {
    body = await request.json();
  } catch {
    throw new HTTPError(400, "The website isn't JSON.");
  }
  const domain = typeof body.domain === "string" ? domainOf(body.domain) : "";
  if (!domain) throw new HTTPError(400, "The domain is missing or isn't a domain.");
  const name = (typeof body.name === "string" && body.name.trim()) || domain;
  const id = crypto.randomUUID();
  await env.DB.prepare("INSERT INTO website (id, name, domain, created_at) VALUES (?, ?, ?, ?)")
    .bind(id, name.slice(0, 100), domain, now)
    .run();
  return website(env, id);
}

/** updateWebsite renames a website or changes its domain, as Umami's POST /websites/:id does; what the body leaves out stays. */
async function updateWebsite(env: Env, id: string, request: Request): Promise<Website> {
  const current = await website(env, id);
  let body: { name?: unknown; domain?: unknown };
  try {
    body = await request.json();
  } catch {
    throw new HTTPError(400, "The website isn't JSON.");
  }
  let domain = current.domain;
  if (body.domain !== undefined) {
    domain = typeof body.domain === "string" ? domainOf(body.domain) : "";
    if (!domain) throw new HTTPError(400, "The domain is missing or isn't a domain.");
  }
  let name = current.name;
  if (body.name !== undefined) {
    name = (typeof body.name === "string" && body.name.trim()) || domain;
  }
  await env.DB.prepare("UPDATE website SET name = ?, domain = ? WHERE id = ?").bind(name.slice(0, 100), domain, id).run();
  return website(env, id);
}

async function deleteWebsite(env: Env, id: string, origin: string, f: typeof fetch) {
  const w = await website(env, id);
  // A website the Worker tracks loses its routes first, so its site no longer passes through the Worker for nothing; a failure there leaves them for the next deploy to take away.
  let warning = "";
  if (w.autoTrack) {
    try {
      await removeCloudflareSite(env, f, w.domain, origin);
    } catch (e) {
      warning = e instanceof Error ? e.message : String(e);
    }
  }
  await env.DB.batch([
    env.DB.prepare("DELETE FROM event WHERE website_id = ?").bind(id),
    env.DB.prepare("DELETE FROM website WHERE id = ?").bind(id),
  ]);
  forget(id);
  return warning ? { ok: true, warning } : { ok: true };
}

/** ownHosts are the hostnames the server answers Umapp at: SERVER_HOSTS and the one this request came to. */
function ownHosts(env: Env, request: Request): string[] {
  return [...(env.SERVER_HOSTS ?? "").split(","), new URL(request.url).hostname].map((h) => h.trim().toLowerCase()).filter(Boolean);
}

/** cloudflare answers /cloudflare: the accounts' sites, and adding one. */
async function cloudflare(env: Env, request: Request, parts: string[], f: typeof fetch, now: number): Promise<Response> {
  const m = request.method;
  if (parts.length === 1 && m === "GET") return json({ sites: await listCloudflareSites(env, f, ownHosts(env, request)) });
  if (parts.length === 2 && parts[1] === "sites" && m === "POST") {
    let body: { domain?: unknown };
    try {
      body = await request.json();
    } catch {
      throw new HTTPError(400, "The site isn't JSON.");
    }
    const id = await addCloudflareSite(env, f, typeof body.domain === "string" ? body.domain : "", new URL(request.url).origin, ownHosts(env, request), now);
    return json(await website(env, id));
  }
  throw new HTTPError(parts.length > 2 ? 404 : 405, parts.length > 2 ? "Not found." : "Method not allowed.");
}

/** api answers path, under /api, of an authorized request. */
export async function api(request: Request, env: Env, path: string, now = Date.now(), f: typeof fetch = fetch): Promise<Response> {
  if (!env.API_KEY) return json({ message: "The server has no API_KEY." }, 500);
  if (!(await authorized(request, env.API_KEY))) return json({ message: "The API key was rejected." }, 401);
  const q = new URL(request.url).searchParams;
  const m = request.method;
  const parts = path.split("/").filter(Boolean);
  try {
    if (parts[0] === "cloudflare") return await cloudflare(env, request, parts, f, now);
    if (parts[0] !== "websites") throw new HTTPError(404, "Not found.");
    const [, rawID, sub] = parts;
    if (parts.length > 3) throw new HTTPError(404, "Not found.");
    if (!rawID) {
      if (m === "GET") return json(await websites(env));
      if (m === "POST") return json(await createWebsite(env, request, now));
      throw new HTTPError(405, "Method not allowed.");
    }
    const id = decodeURIComponent(rawID);
    if (!sub) {
      if (m === "GET") return json(await website(env, id));
      if (m === "POST") return json(await updateWebsite(env, id, request));
      if (m === "DELETE") return json(await deleteWebsite(env, id, new URL(request.url).origin, f));
      throw new HTTPError(405, "Method not allowed.");
    }
    if (m !== "GET") throw new HTTPError(405, "Method not allowed.");
    await website(env, id);
    switch (sub) {
      case "stats":
        return json(await stats(env, id, q));
      case "active":
        return json(await active(env, id, now));
      case "pageviews":
        return json(await pageviews(env, id, q));
      case "metrics":
        return json(await metrics(env, id, q));
    }
    throw new HTTPError(404, "Not found.");
  } catch (e) {
    if (e instanceof HTTPError || e instanceof CloudflareError) return json({ message: e.message }, e.status);
    throw e;
  }
}
