  const MAX_ORCH_MODELS = 3;

  const ORCH_SLOT_LABELS = ["Primary", "Fallback 2", "Fallback 3"];

  /** @type {HTMLElement | null} */
  let openProvDropdown = null;

  function closeProvDropdowns() {
    if (openProvDropdown) {
      openProvDropdown.classList.remove("open");
      openProvDropdown = null;
    }
  }

  document.addEventListener("click", closeProvDropdowns);
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeProvDropdowns();
  });

  /** @param {number | undefined} n */
  function formatContextTokens(n) {
    if (!n || n <= 0) return "";
    if (n >= 1_000_000) {
      const m = n / 1_000_000;
      return (m >= 10 ? Math.round(m) : Math.round(m * 10) / 10) + "M ctx";
    }
    if (n >= 1000) {
      const k = n / 1000;
      return (k >= 100 ? Math.round(k) : Math.round(k * 10) / 10) + "k ctx";
    }
    return n + " ctx";
  }

  /** @param {any} p */
  function providerBadgeMeta(p) {
    if (!p) return { text: "—", className: "disabled" };
    if (p.models_error) return { text: "error", className: "error", title: p.models_error };
    if (p.active) return { text: "active", className: "running" };
    if (p.ready && p.model_count > 0) {
      return { text: `${p.model_count} models`, className: "ok" };
    }
    if (p.ready) return { text: "ready", className: "ready" };
    if (p.needs_key && !p.api_key_set) return { text: "needs key", className: "disabled" };
    if (p.custom && !p.api_base) return { text: "needs URL", className: "disabled" };
    return { text: "—", className: "disabled" };
  }

  /** @param {string} key @param {string} [extraClass] */
  function makeProviderIconEl(key, extraClass) {
    const span = document.createElement("span");
    span.className = "prov-icon" + (extraClass ? " " + extraClass : "");
    span.innerHTML = providerLogoHtml(key);
    span.title = key || "global";
    span.setAttribute("aria-hidden", "true");
    return span;
  }

  /** @param {HTMLElement} iconEl @param {string} key */
  function setProviderIconEl(iconEl, key) {
    iconEl.innerHTML = providerLogoHtml(key);
    iconEl.title = key || "global";
  }

  /** @param {any} p */
  function appendProviderBadge(parent, p) {
    const meta = providerBadgeMeta(p);
    const badge = document.createElement("span");
    badge.className = "badge " + meta.className;
    badge.textContent = meta.text;
    if (meta.title) badge.title = meta.title;
    parent.appendChild(badge);
  }

  /**
   * Custom provider picker with logos (replaces native select in orchestra).
   * @param {string} value
   * @param {(key: string) => void} onChange
   * @param {{ key: string, label: string }[]} options
   */
  function buildProviderDropdown(value, onChange, options) {
    const root = document.createElement("div");
    root.className = "prov-dropdown";

    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "prov-dropdown-btn";

    const btnIcon = makeProviderIconEl(value, "prov-dropdown-icon");
    const btnLabel = document.createElement("span");
    btnLabel.className = "prov-dropdown-label";
    const btnChevron = document.createElement("span");
    btnChevron.className = "prov-dropdown-chevron";
    btnChevron.textContent = "▾";

    btn.appendChild(btnIcon);
    btn.appendChild(btnLabel);
    btn.appendChild(btnChevron);

    const menu = document.createElement("div");
    menu.className = "prov-dropdown-menu";
    menu.setAttribute("role", "listbox");

    /** @param {string} key */
    function labelFor(key) {
      const hit = options.find((o) => o.key === key);
      return hit ? hit.label : key || "Main (global llm)";
    }

    /** @param {string} key */
    function syncButton(key) {
      setProviderIconEl(btnIcon, key);
      btnLabel.textContent = labelFor(key);
      menu.querySelectorAll(".prov-dropdown-item").forEach((item) => {
        item.classList.toggle("selected", item.getAttribute("data-key") === key);
      });
    }

    options.forEach((o) => {
      const item = document.createElement("button");
      item.type = "button";
      item.className = "prov-dropdown-item" + (o.key === value ? " selected" : "");
      item.setAttribute("data-key", o.key);
      item.setAttribute("role", "option");
      item.appendChild(makeProviderIconEl(o.key, "prov-dropdown-icon"));
      const lab = document.createElement("span");
      lab.className = "prov-dropdown-label";
      lab.textContent = o.label;
      item.appendChild(lab);
      item.addEventListener("click", (e) => {
        e.stopPropagation();
        syncButton(o.key);
        onChange(o.key);
        closeProvDropdowns();
      });
      menu.appendChild(item);
    });

    syncButton(value || "");

    btn.addEventListener("click", (e) => {
      e.stopPropagation();
      const wasOpen = root.classList.contains("open");
      closeProvDropdowns();
      if (!wasOpen) {
        root.classList.add("open");
        openProvDropdown = root;
      }
    });

    root.appendChild(btn);
    root.appendChild(menu);
    return root;
  }
