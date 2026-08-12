  /** Catalog keys with bundled SVG logos under media/provider-icons/. */
  const PROVIDER_ICON_FILES = new Set([
    "",
    "lmstudio",
    "ollama",
    "vllm",
    "openrouter",
    "openai",
    "anthropic",
    "google",
    "mistral",
    "deepseek",
    "xai",
    "moonshot",
    "groq",
    "together",
    "fireworks",
    "cerebras",
    "custom",
  ]);

  /** @param {string} key */
  function providerIconFile(key) {
    const k = (key || "").toLowerCase();
    if (k === "") return "default";
    if (PROVIDER_ICON_FILES.has(k)) return k;
    return "";
  }

  /** @param {string} key */
  function providerLogoHtml(key) {
    const k = (key || "").toLowerCase();
    const file = providerIconFile(k);
    const base =
      typeof window !== "undefined" && window.__ORCH_ICON_BASE
        ? String(window.__ORCH_ICON_BASE)
        : "";
    if (base && file) {
      const ver =
        typeof window !== "undefined" && window.__ORCH_ICON_V
          ? String(window.__ORCH_ICON_V)
          : "";
      const src = base.replace(/\/?$/, "/") + file + ".svg" + (ver ? "?v=" + encodeURIComponent(ver) : "");
      return `<img class="prov-logo-img" src="${src}" alt="" width="22" height="22" loading="lazy" decoding="async" />`;
    }
    if (k) {
      const letter = k.slice(0, 1).toUpperCase();
      return `<svg viewBox="0 0 24 24" aria-hidden="true"><rect width="24" height="24" rx="6" fill="rgba(255,255,255,0.08)"/><text x="12" y="16" text-anchor="middle" fill="#b9bac2" font-size="11" font-family="Segoe UI,sans-serif" font-weight="600">${letter}</text></svg>`;
    }
    return `<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" stroke="rgba(255,255,255,0.35)" stroke-width="1.5" fill="none"/><path d="M8 12h8M12 8v8" stroke="#b9bac2" stroke-width="1.5" stroke-linecap="round"/></svg>`;
  }
