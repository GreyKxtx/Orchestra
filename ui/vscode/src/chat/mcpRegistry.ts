/**
 * Official MCP Registry client (https://registry.modelcontextprotocol.io).
 * Maps server.json packages → Orchestra stdio install payloads.
 */

export type McpCatalogEntry = {
  id: string;
  name: string;
  title: string;
  description: string;
  category: string;
  command: string;
  env: string[];
  envRequired: boolean;
  homepage?: string;
  icon?: string;
  tags: string[];
  version?: string;
  /** false for remote-only servers — Orchestra currently supports stdio only. */
  installable: boolean;
  source: "registry" | "local";
};

export type McpCatalogPayload = {
  version: number;
  entries: McpCatalogEntry[];
  nextCursor?: string;
  source: "registry" | "local" | "mixed";
  error?: string;
  search?: string;
  /** True while extension is still pulling more registry pages. */
  prefetching?: boolean;
  loadedCount?: number;
};

type RegistryPackage = {
  registryType?: string;
  identifier?: string;
  version?: string;
  runtimeHint?: string;
  runtimeArguments?: RegistryArg[];
  packageArguments?: RegistryArg[];
  environmentVariables?: RegistryEnv[];
  transport?: { type?: string; url?: string };
};

type RegistryArg = {
  type?: string;
  name?: string;
  value?: string;
  valueHint?: string;
  default?: string;
  isRequired?: boolean;
  format?: string;
};

type RegistryEnv = {
  name?: string;
  value?: string;
  default?: string;
  description?: string;
  isRequired?: boolean;
  isSecret?: boolean;
};

type RegistryServer = {
  name?: string;
  title?: string;
  description?: string;
  version?: string;
  websiteUrl?: string;
  icons?: Array<{ src?: string; mimeType?: string; theme?: string }>;
  repository?: { url?: string; source?: string };
  packages?: RegistryPackage[];
  remotes?: Array<{ type?: string; url?: string }>;
};

type RegistryListResponse = {
  servers?: Array<{ server?: RegistryServer }>;
  metadata?: { nextCursor?: string; count?: number };
};

const REGISTRY_BASE = "https://registry.modelcontextprotocol.io/v0.1/servers";

export async function fetchMcpRegistryCatalog(opts: {
  search?: string;
  cursor?: string;
  limit?: number;
  workspaceRoot?: string;
}): Promise<McpCatalogPayload> {
  const limit = Math.min(Math.max(opts.limit ?? 40, 1), 100);
  const params = new URLSearchParams();
  params.set("version", "latest");
  params.set("limit", String(limit));
  const search = (opts.search || "").trim();
  if (search) {
    params.set("search", search);
  }
  if (opts.cursor) {
    params.set("cursor", opts.cursor);
  }

  const url = `${REGISTRY_BASE}?${params.toString()}`;
  const res = await fetch(url, {
    headers: { Accept: "application/json" },
  });
  if (!res.ok) {
    throw new Error(`MCP registry HTTP ${res.status}`);
  }
  const data = (await res.json()) as RegistryListResponse;
  const root = opts.workspaceRoot || ".";
  const entries: McpCatalogEntry[] = [];
  for (const row of data.servers || []) {
    const mapped = mapRegistryServer(row.server, root);
    if (mapped) {
      entries.push(mapped);
    }
  }
  return {
    version: 1,
    entries,
    nextCursor: data.metadata?.nextCursor || undefined,
    source: "registry",
    search,
  };
}

export function mapLocalCatalog(
  local: { entries?: unknown[] } | null | undefined,
  workspaceRoot?: string
): McpCatalogEntry[] {
  const root = workspaceRoot || ".";
  const raw = Array.isArray(local?.entries) ? local!.entries! : [];
  const out: McpCatalogEntry[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const e = item as Record<string, unknown>;
    const id = String(e.id || e.name || "").trim();
    if (!id) {
      continue;
    }
    const command = String(e.command || "").replace(/\$\{workspaceRoot\}/g, root);
    const env = Array.isArray(e.env) ? e.env.map((x) => String(x)) : [];
    const tags = Array.isArray(e.tags)
      ? e.tags.map((x) => String(x))
      : deriveLocalTags(e);
    out.push({
      id,
      name: String(e.name || id),
      title: String(e.title || e.name || id),
      description: String(e.description || ""),
      category: String(e.category || "Local"),
      command,
      env,
      envRequired: Boolean(e.envRequired),
      homepage: e.homepage ? String(e.homepage) : undefined,
      icon: e.icon ? String(e.icon) : undefined,
      tags,
      version: e.version ? String(e.version) : undefined,
      installable: e.installable === false ? false : Boolean(command),
      source: "local",
    });
  }
  return out;
}

function deriveLocalTags(e: Record<string, unknown>): string[] {
  const tags: string[] = ["featured"];
  if (e.category) {
    tags.push(String(e.category).toLowerCase());
  }
  if (e.envRequired) {
    tags.push("needs-key");
  }
  tags.push("stdio");
  return uniqueTags(tags);
}

function mapRegistryServer(server: RegistryServer | undefined, workspaceRoot: string): McpCatalogEntry | null {
  if (!server?.name) {
    return null;
  }
  const stdioPkg = pickStdioPackage(server.packages || []);
  const install = stdioPkg ? buildStdioInstall(stdioPkg, workspaceRoot) : null;
  const hasRemote = Array.isArray(server.remotes) && server.remotes.length > 0;
  const tags = buildTags(server, stdioPkg, hasRemote, install);
  const shortName = shortServerName(server.name, stdioPkg?.identifier);
  const title = (server.title || "").trim() || humanizeName(shortName);
  const icon = pickIcon(server.icons);
  const homepage =
    server.websiteUrl ||
    server.repository?.url ||
    (stdioPkg?.identifier && stdioPkg.registryType === "npm"
      ? `https://www.npmjs.com/package/${stdioPkg.identifier}`
      : undefined);

  return {
    id: server.name,
    name: shortName,
    title,
    description: (server.description || "").trim(),
    category: categorize(tags, hasRemote, Boolean(install)),
    command: install?.command || "",
    env: install?.env || [],
    envRequired: Boolean(install?.envRequired) || (!install && hasRemote),
    homepage,
    icon,
    tags,
    version: (server.version || stdioPkg?.version || "").trim() || undefined,
    installable: Boolean(install?.command),
    source: "registry",
  };
}

function pickStdioPackage(packages: RegistryPackage[]): RegistryPackage | null {
  const stdio = packages.filter((p) => (p.transport?.type || "stdio") === "stdio" && p.identifier);
  if (!stdio.length) {
    return null;
  }
  const prefer = (types: string[]) => stdio.find((p) => types.includes((p.registryType || "").toLowerCase()));
  return prefer(["npm"]) || prefer(["pypi"]) || prefer(["nuget"]) || prefer(["oci", "docker"]) || stdio[0] || null;
}

function buildStdioInstall(
  pkg: RegistryPackage,
  workspaceRoot: string
): { command: string; env: string[]; envRequired: boolean } | null {
  const id = (pkg.identifier || "").trim();
  if (!id) {
    return null;
  }
  const type = (pkg.registryType || "").toLowerCase();
  const runtimeArgs = resolveArgs(pkg.runtimeArguments || [], workspaceRoot);
  const packageArgs = resolveArgs(pkg.packageArguments || [], workspaceRoot);
  const needsArgConfig = [...(pkg.runtimeArguments || []), ...(pkg.packageArguments || [])].some(
    (a) => a.isRequired && !a.value && !a.default && !guessArgValue(a, workspaceRoot)
  );

  let parts: string[] = [];
  if (type === "npm") {
    const hint = (pkg.runtimeHint || "npx").trim() || "npx";
    parts = [hint, ...runtimeArgs];
    if (hint === "npx" && !runtimeArgs.includes("-y")) {
      parts.push("-y");
    }
    parts.push(id, ...packageArgs);
  } else if (type === "pypi") {
    const hint = (pkg.runtimeHint || "uvx").trim() || "uvx";
    parts = [hint, ...runtimeArgs, id, ...packageArgs];
  } else if (type === "nuget") {
    const hint = (pkg.runtimeHint || "dnx").trim() || "dnx";
    parts = [hint, ...runtimeArgs, id, ...packageArgs];
  } else if (type === "oci" || type === "docker") {
    const hint = (pkg.runtimeHint || "docker").trim() || "docker";
    parts = [hint, "run", "-i", "--rm", ...runtimeArgs, id, ...packageArgs];
  } else {
    const hint = (pkg.runtimeHint || "").trim();
    if (!hint) {
      return null;
    }
    parts = [hint, ...runtimeArgs, id, ...packageArgs];
  }

  const envLines: string[] = [];
  let envRequired = needsArgConfig;
  for (const ev of pkg.environmentVariables || []) {
    const name = (ev.name || "").trim();
    if (!name) {
      continue;
    }
    const val = ev.value ?? ev.default ?? "";
    envLines.push(`${name}=${val}`);
    if (ev.isRequired && !val) {
      envRequired = true;
    }
  }

  return {
    command: shellJoin(parts),
    env: envLines,
    envRequired,
  };
}

function resolveArgs(args: RegistryArg[], workspaceRoot: string): string[] {
  const out: string[] = [];
  for (const a of args) {
    const raw = a.value ?? a.default ?? guessArgValue(a, workspaceRoot);
    if (raw === undefined || raw === null || raw === "") {
      continue;
    }
    const value = String(raw).replace(/\{workspaceRoot\}|\{workspaceFolder\}/gi, workspaceRoot);
    if (a.type === "named") {
      const flag = (a.name || "").trim();
      if (!flag) {
        continue;
      }
      if (flag.includes("=")) {
        out.push(flag.replace(/=$/, "") + "=" + value);
      } else {
        out.push(flag, value);
      }
    } else {
      out.push(value);
    }
  }
  return out;
}

function guessArgValue(a: RegistryArg, workspaceRoot: string): string | undefined {
  const hint = `${a.valueHint || ""} ${a.name || ""} ${a.format || ""}`.toLowerCase();
  if (a.format === "filepath" || /path|dir|folder|workspace|repository|root/.test(hint)) {
    return workspaceRoot;
  }
  return undefined;
}

function shellJoin(parts: string[]): string {
  return parts
    .map((p) => {
      if (/[\s"]/u.test(p)) {
        return `"${p.replace(/"/g, '\\"')}"`;
      }
      return p;
    })
    .join(" ");
}

function pickIcon(icons: RegistryServer["icons"]): string | undefined {
  if (!Array.isArray(icons) || !icons.length) {
    return undefined;
  }
  const dark = icons.find((i) => i.theme === "dark" && i.src);
  const any = icons.find((i) => i.src);
  const src = (dark || any)?.src || "";
  if (!src.startsWith("https://")) {
    return undefined;
  }
  return src;
}

function shortServerName(full: string, pkgId?: string): string {
  const fromPkg = (pkgId || "").trim();
  if (fromPkg) {
    const base = fromPkg.includes("/") ? fromPkg.split("/").pop()! : fromPkg;
    return base.replace(/^@/, "").replace(/[^a-zA-Z0-9._-]+/g, "-").slice(0, 48) || "mcp";
  }
  const parts = full.split("/");
  const last = parts[parts.length - 1] || full;
  return last.replace(/[^a-zA-Z0-9._-]+/g, "-").slice(0, 48) || "mcp";
}

function humanizeName(name: string): string {
  return name
    .replace(/[-_]+/g, " ")
    .replace(/\bmcp\b/gi, "MCP")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

function buildTags(
  server: RegistryServer,
  pkg: RegistryPackage | null,
  hasRemote: boolean,
  install: { envRequired: boolean } | null
): string[] {
  const tags: string[] = [];
  if (pkg?.registryType) {
    tags.push(pkg.registryType.toLowerCase());
  }
  if (pkg) {
    tags.push("stdio");
  }
  if (hasRemote) {
    tags.push("remote");
  }
  if (!pkg && hasRemote) {
    tags.push("unsupported");
  }
  if (install?.envRequired) {
    tags.push("needs-key");
  }
  if (server.name?.startsWith("io.modelcontextprotocol/") || server.name?.startsWith("io.github.modelcontextprotocol/")) {
    tags.push("official");
  }
  if (server.repository?.source) {
    tags.push(String(server.repository.source).toLowerCase());
  }
  return uniqueTags(tags);
}

function categorize(tags: string[], hasRemote: boolean, installable: boolean): string {
  if (tags.includes("official")) {
    return "Official";
  }
  if (installable) {
    return "Installable";
  }
  if (hasRemote) {
    return "Remote";
  }
  return "Registry";
}

function uniqueTags(tags: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const t of tags) {
    const k = t.trim().toLowerCase();
    if (!k || seen.has(k)) {
      continue;
    }
    seen.add(k);
    out.push(k);
  }
  return out.slice(0, 6);
}

/** Dedupe by id/name, keep earlier entries (featured first). */
export function mergeMcpEntries(primary: McpCatalogEntry[], extra: McpCatalogEntry[]): McpCatalogEntry[] {
  const out: McpCatalogEntry[] = [];
  const seen = new Set<string>();
  for (const list of [primary, extra]) {
    for (const e of list) {
      const key = String(e.id || e.name || "")
        .trim()
        .toLowerCase();
      if (!key || seen.has(key)) {
        continue;
      }
      // Also skip duplicate short names when ids differ (featured vs registry).
      const nameKey = `name:${String(e.name || "").trim().toLowerCase()}`;
      if (e.name && seen.has(nameKey) && e.source === "registry") {
        continue;
      }
      seen.add(key);
      if (e.name) {
        seen.add(nameKey);
      }
      out.push(e);
    }
  }
  return out;
}

/** Copy registry versions onto featured locals with the same short name. */
export function enrichFeaturedVersions(
  featured: McpCatalogEntry[],
  remote: McpCatalogEntry[]
): McpCatalogEntry[] {
  return featured.map((f) => {
    if (f.version) {
      return f;
    }
    const fname = String(f.name || "").trim().toLowerCase();
    const fid = String(f.id || "").trim().toLowerCase();
    const hit = remote.find((e) => {
      const name = String(e.name || "").trim().toLowerCase();
      const id = String(e.id || "").trim().toLowerCase();
      const cmd = String(e.command || "").toLowerCase();
      if (!e.version) {
        return false;
      }
      if (fname && (name === fname || id.endsWith("/" + fname) || id.endsWith("." + fname))) {
        return true;
      }
      if (fid && (id === fid || name === fid)) {
        return true;
      }
      if (fname && (cmd.includes(`server-${fname}`) || cmd.includes(`/${fname}`) || cmd.endsWith(fname))) {
        return true;
      }
      return false;
    });
    return hit?.version ? { ...f, version: hit.version } : f;
  });
}
