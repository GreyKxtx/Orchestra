  // ---- the Graph view -------------------------------------------------------
  //
  // A third segment beside Chat and Trajectory: the shape of the project, read
  // from the code knowledge graph the core keeps (.orchestra/ckg.db, over
  // index.graph). The workspace sits in the middle with the files that answer
  // to no folder; every ring outwards is one more level of nesting, and a
  // curve across the rings says symbols in one file call or use symbols in the
  // other — its width how many. Beside the picture a readout of what the index
  // holds (files, folders, symbols, tests, languages) and, for whatever is
  // selected, its neighbours and — for a file — its functions with the first
  // lines of each, read back with index.outline.
  //
  // Built at runtime, like the project label in the header: everything inside
  // #app is byte-identical with the VS Code webview's markup, and the editor
  // has its own graph viewer. The shared switch (05f-trajectory.js) knows two
  // views and stamps data-view on #app; this segment stamps "graph" the same
  // way and follows the attribute back, so either side's click leaves exactly
  // one segment selected.

  const GRAPH_LEVEL = "file";
  /** Past this many drawn nodes the picture folds a level of nesting away. */
  const GRAPH_VISIBLE_CAP = 2600;
  /** Only the heaviest relations are drawn; the rest are in the readout. */
  const GRAPH_LINK_CAP = 600;
  /** Rings never closer than this, nor further apart. */
  const GRAPH_RING_MIN = 110;
  const GRAPH_RING_MAX = 460;

  const graphApp = document.getElementById("app");
  const graphSwitchEl = document.getElementById("view-switch");
  const graphTrajectoryBtn = document.getElementById("view-trajectory-btn");
  const graphChatBtn = document.getElementById("view-chat-btn");
  const graphTrajectoryPane = document.getElementById("trajectory");

  /** @type {any} */ let graphBtn = null;
  /** @type {any} */ let graphPane = null;
  /** @type {any} */ let graphStage = null;
  /** @type {any} */ let graphCanvas = null;
  /** @type {any} */ let graphStatsEl = null;
  /** @type {any} */ let graphHintEl = null;
  /** @type {any} */ let graphCardEl = null;
  /** @type {any} */ let graphSideEl = null;
  /** @type {any} */ let graphFilesBtn = null;
  /** @type {any} */ let graphLinksBtn = null;
  /** @type {any} */ let graphDepthOutEl = null;

  /** @type {{projectId: string, available: boolean, nodes: any[], links: any[], stats: any} | null} */
  let graphData = null;
  /** @type {any} */ let graphTree = null;
  /** @type {any} */ let graphLayout = null;
  let graphLoading = false;
  let graphShowFiles = true;
  let graphShowLinks = true;
  let graphDepth = 3;
  let graphDrawQueued = false;
  /** @type {any} */ let graphHover = null;
  /** @type {string} */ let graphSelectedId = "";
  /** @type {any} */ let graphDrag = null;
  /** @type {any} */ let graphOutline = null;
  let graphOutlineSeq = 0;
  let graphOpenSymbol = -1;

  function graphViewActive() {
    return Boolean(graphApp && graphApp.dataset && graphApp.dataset.view === "graph");
  }

  const GRAPH_ICON =
    '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true">' +
    '<circle cx="6" cy="18" r="2.4" stroke="currentColor" stroke-width="2"/>' +
    '<circle cx="12" cy="6" r="2.4" stroke="currentColor" stroke-width="2"/>' +
    '<circle cx="18" cy="18" r="2.4" stroke="currentColor" stroke-width="2"/>' +
    '<path d="M7.4 16 10.6 8.2M13.4 8.2l3.2 7.8M8.4 18h7.2" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>' +
    "</svg>";

  /** One colour per language, so a ring of files says what it is made of. */
  const GRAPH_LANG_COLORS = {
    go: "#7fd1e8",
    ts: "#6aa6f5",
    tsx: "#6aa6f5",
    js: "#e6c368",
    jsx: "#e6c368",
    mjs: "#e6c368",
    cjs: "#e6c368",
    py: "#66c288",
    rs: "#e09660",
    java: "#e08484",
    kt: "#c08ae8",
    rb: "#e07a7a",
    php: "#9a8ae0",
    c: "#8fb6d8",
    h: "#8fb6d8",
    cc: "#8fb6d8",
    cpp: "#8fb6d8",
    hpp: "#8fb6d8",
    cs: "#79c6a8",
    css: "#6ad0b0",
    scss: "#6ad0b0",
    html: "#e0906a",
    md: "#9a9aa4",
    json: "#b294e0",
    yml: "#b294e0",
    yaml: "#b294e0",
    toml: "#b294e0",
    sql: "#d0a05a",
    sh: "#86bf86",
  };

  /** @param {string} id */
  function graphExtOf(id) {
    const base = id.slice(id.lastIndexOf("/") + 1);
    const dot = base.lastIndexOf(".");
    return dot > 0 ? base.slice(dot + 1).toLowerCase() : "";
  }

  /** @param {string} id */
  function graphColorFor(id) {
    return GRAPH_LANG_COLORS[graphExtOf(id)] || "";
  }

  function ensureGraphView() {
    if (graphBtn || !graphApp || !graphSwitchEl || !graphSwitchEl.appendChild) {
      return;
    }
    graphBtn = document.createElement("button");
    graphBtn.type = "button";
    graphBtn.className = "view-segment";
    graphBtn.setAttribute("role", "tab");
    graphBtn.setAttribute("aria-selected", "false");
    // A fixed string, none of it from data.
    graphBtn.innerHTML = GRAPH_ICON + "Graph";
    graphBtn.addEventListener("click", () => showGraphView());
    graphBtn.addEventListener("keydown", (e) => {
      if (e.key === "ArrowLeft" && graphTrajectoryBtn && graphTrajectoryBtn.click) {
        e.preventDefault();
        graphTrajectoryBtn.click();
        if (graphTrajectoryBtn.focus) graphTrajectoryBtn.focus();
      }
    });
    if (graphTrajectoryBtn && graphTrajectoryBtn.parentNode === graphSwitchEl && graphSwitchEl.insertBefore) {
      graphSwitchEl.insertBefore(graphBtn, graphTrajectoryBtn.nextSibling);
    } else {
      graphSwitchEl.appendChild(graphBtn);
    }

    graphPane = document.createElement("div");
    graphPane.className = "graph-pane";
    graphPane.setAttribute("role", "tabpanel");
    graphPane.setAttribute("aria-label", "Project graph");

    const toolbar = document.createElement("div");
    toolbar.className = "graph-toolbar";
    graphStatsEl = document.createElement("span");
    graphStatsEl.className = "graph-stats";

    const depth = document.createElement("span");
    depth.className = "graph-depth";
    const less = graphToolButton("−", "One level of nesting less", () => stepGraphDepth(-1));
    graphDepthOutEl = document.createElement("span");
    graphDepthOutEl.className = "graph-depth-value";
    const more = graphToolButton("+", "One level of nesting more", () => stepGraphDepth(1));
    depth.append(less, graphDepthOutEl, more);

    graphFilesBtn = graphToolButton("Files", "Draw the files, not only the folders", () => {
      graphShowFiles = !graphShowFiles;
      layoutGraph(true);
      renderGraphSide();
      scheduleGraphDraw();
    });
    graphLinksBtn = graphToolButton("Links", "Draw the calls between files", () => {
      graphShowLinks = !graphShowLinks;
      syncGraphControls();
      scheduleGraphDraw();
    });
    const fit = graphToolButton("Fit", "Fit the whole graph in view", () => {
      if (graphLayout) {
        graphLayout.fitPending = true;
        scheduleGraphDraw();
      }
    });
    const refresh = graphToolButton("Refresh", "Read the graph again", () => void loadGraph(true));
    toolbar.append(graphStatsEl, depth, graphFilesBtn, graphLinksBtn, fit, refresh);

    const body = document.createElement("div");
    body.className = "graph-body";
    graphStage = document.createElement("div");
    graphStage.className = "graph-stage";
    graphCanvas = document.createElement("canvas");
    graphCanvas.className = "graph-canvas";
    graphHintEl = document.createElement("div");
    graphHintEl.className = "graph-hint";
    graphHintEl.hidden = true;
    graphCardEl = document.createElement("div");
    graphCardEl.className = "graph-card";
    graphCardEl.hidden = true;
    graphStage.append(graphCanvas, graphHintEl, graphCardEl);
    graphSideEl = document.createElement("aside");
    graphSideEl.className = "graph-side";
    body.append(graphStage, graphSideEl);
    graphPane.append(toolbar, body);

    if (graphTrajectoryPane && graphTrajectoryPane.parentNode === graphApp && graphApp.insertBefore) {
      graphApp.insertBefore(graphPane, graphTrajectoryPane.nextSibling);
    } else {
      graphApp.appendChild(graphPane);
    }
    bindGraphCanvas();
    syncGraphControls();
    renderGraphSide();

    if (typeof MutationObserver === "function" && graphApp.dataset) {
      new MutationObserver(() => syncGraphSegment()).observe(graphApp, {
        attributes: true,
        attributeFilter: ["data-view"],
      });
    }
    if (typeof ResizeObserver === "function") {
      new ResizeObserver(() => scheduleGraphDraw()).observe(graphPane);
    }
  }

  /** @param {string} label @param {string} title @param {() => void} onClick */
  function graphToolButton(label, title, onClick) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "graph-tool";
    b.textContent = label;
    b.title = title;
    b.addEventListener("click", onClick);
    return b;
  }

  function showGraphView() {
    ensureGraphView();
    if (!graphApp || !graphApp.dataset) {
      return;
    }
    graphApp.dataset.view = "graph";
    syncGraphSegment();
    void loadGraph(false);
  }

  /** One selected segment, whichever side stamped the view. */
  function syncGraphSegment() {
    const active = graphViewActive();
    if (graphBtn && graphBtn.setAttribute) {
      graphBtn.setAttribute("aria-selected", active ? "true" : "false");
    }
    if (active) {
      for (const b of [graphChatBtn, graphTrajectoryBtn]) {
        if (b && b.setAttribute) b.setAttribute("aria-selected", "false");
      }
      void loadGraph(false);
      scheduleGraphDraw();
    }
  }

  /** @param {number} by */
  function stepGraphDepth(by) {
    if (!graphTree) return;
    const next = Math.max(1, Math.min(graphTree.maxDepth, graphDepth + by));
    if (next === graphDepth) return;
    graphDepth = next;
    layoutGraph(true);
    renderGraphSide();
    scheduleGraphDraw();
  }

  function syncGraphControls() {
    if (graphDepthOutEl) {
      const max = graphTree ? graphTree.maxDepth : graphDepth;
      graphDepthOutEl.textContent = "levels " + graphDepth + "/" + max;
      graphDepthOutEl.title = "How many levels of nesting the rings go out to";
    }
    if (graphFilesBtn) {
      graphFilesBtn.classList.toggle("on", graphShowFiles);
      graphFilesBtn.title = graphShowFiles
        ? "Draw folders only, with the calls between them summed up"
        : "Draw every file, not only the folders";
    }
    if (graphLinksBtn) {
      graphLinksBtn.classList.toggle("on", graphShowLinks);
      graphLinksBtn.title = graphShowLinks
        ? "Leave out the calls between files, keeping the nesting"
        : "Draw the calls between files again";
    }
    if (graphStatsEl && graphLayout) {
      const folders = graphLayout.nodes.filter((n) => n.group === "folder").length;
      const files = graphLayout.nodes.filter((n) => n.group === "file").length;
      const drawn = graphLayout.relationsDrawn;
      const total = graphLayout.relationsTotal;
      graphStatsEl.textContent =
        folders + " folders · " + files + " files · " +
        (drawn < total ? "the " + drawn + " heaviest of " + total + " links" : drawn + " links");
    }
  }

  /** @param {string} text */
  function setGraphHint(text) {
    if (!graphHintEl) return;
    graphHintEl.textContent = text;
    graphHintEl.hidden = !text;
  }

  /** @param {boolean} force */
  async function loadGraph(force) {
    const projectId = currentProjectId;
    const conn = projectId ? connFor(projectId) : null;
    if (!conn || !conn.isOpen()) {
      graphData = null;
      graphTree = null;
      graphLayout = null;
      setGraphHint(pendingOpen() ? "The workspace is still opening…" : "No workspace is open.");
      if (graphStatsEl) graphStatsEl.textContent = "";
      renderGraphSide();
      return;
    }
    if (!force && graphData && graphData.projectId === projectId) {
      return;
    }
    if (graphLoading) {
      return;
    }
    graphLoading = true;
    setGraphHint("Reading the project graph…");
    try {
      const r = (await conn.send("index.graph", { level: GRAPH_LEVEL })) || {};
      if (projectId !== currentProjectId) {
        return; // the user has moved on; the next show reads that project's
      }
      graphData = {
        projectId,
        available: Boolean(r.available),
        nodes: Array.isArray(r.nodes) ? r.nodes : [],
        links: Array.isArray(r.links) ? r.links : [],
        stats: r.stats && typeof r.stats === "object" ? r.stats : {},
      };
      graphSelectedId = "";
      graphOutline = null;
      graphOpenSymbol = -1;
      buildGraphTree();
      layoutGraph(true);
      if (!graphData.available || graphData.nodes.length === 0) {
        setGraphHint("Nothing is indexed yet. Settings → Index & Graph → Rebuild graph, then Refresh here.");
      } else {
        setGraphHint("");
      }
      renderGraphSide();
      scheduleGraphDraw();
    } catch (err) {
      if (projectId === currentProjectId) {
        setGraphHint("Could not read the graph: " + String((err && err.message) || err));
      }
    } finally {
      graphLoading = false;
    }
  }

  // A project switch or a new session clears the transcript; the graph is
  // the project's, so it follows the same signal rather than activateProject.
  window.addEventListener("message", (ev) => {
    if (ev.data && ev.data.type === "clearMessages" && graphViewActive()) {
      void loadGraph(false);
    }
  });

  /* ---- the tree ------------------------------------------------------------- */

  /** @param {any} n */
  function graphSymbolCount(n) {
    const m = n && n.meta ? n.meta : {};
    for (const k of Object.keys(m)) {
      if (/символ|symbol/i.test(k) && typeof m[k] === "number") return m[k];
    }
    return 0;
  }

  /** @param {string} id */
  function graphParentOf(id) {
    const i = id.lastIndexOf("/");
    return i > 0 ? id.slice(0, i) : "";
  }

  function graphProjectName() {
    const entry = typeof known !== "undefined" ? known.find((p) => p.id === currentProjectId) : null;
    return (entry && (entry.name || entry.path)) || "workspace";
  }

  /**
   * Folders and files as one tree, the workspace at its root. Every folder in
   * a file's path exists even when the graph named only some of them, so a
   * ring is a level of nesting and nothing hangs off nowhere.
   */
  function buildGraphTree() {
    const root = {
      id: "",
      name: graphProjectName(),
      group: "root",
      depth: 0,
      parent: null,
      children: [],
      symbols: 0,
      files: 0,
      subFiles: 0,
      subSymbols: 0,
      leaves: 0,
      meta: {},
    };
    const byId = new Map([["", root]]);

    const folder = (id) => {
      const have = byId.get(id);
      if (have) return have;
      const parentId = graphParentOf(id);
      const parent = folder(parentId);
      const node = {
        id,
        name: id.slice(parentId ? parentId.length + 1 : 0) || id,
        group: "folder",
        depth: parent.depth + 1,
        parent,
        children: [],
        symbols: 0,
        files: 0,
        subFiles: 0,
        subSymbols: 0,
        leaves: 0,
        meta: {},
      };
      byId.set(id, node);
      parent.children.push(node);
      return node;
    };

    const nodes = graphData ? graphData.nodes : [];
    for (const raw of nodes) {
      if (raw.group === "folder" && raw.id) {
        const f = folder(raw.id);
        f.meta = raw.meta || {};
        if (raw.name) f.name = raw.name;
      }
    }
    for (const raw of nodes) {
      if (raw.group !== "file" || !raw.id) continue;
      const parent = folder(graphParentOf(raw.id));
      const node = {
        id: raw.id,
        name: raw.name || raw.id.slice(raw.id.lastIndexOf("/") + 1),
        group: "file",
        depth: parent.depth + 1,
        parent,
        children: [],
        symbols: graphSymbolCount(raw),
        files: 0,
        subFiles: 1,
        subSymbols: 0,
        leaves: 1,
        meta: raw.meta || {},
      };
      byId.set(node.id, node);
      parent.children.push(node);
    }

    // Roll the counts up and note how deep the tree runs.
    let maxDepth = 1;
    const roll = (n) => {
      let files = n.group === "file" ? 1 : 0;
      let symbols = n.symbols;
      for (const c of n.children) {
        roll(c);
        files += c.subFiles;
        symbols += c.subSymbols;
      }
      n.subFiles = files;
      n.subSymbols = symbols;
      if (n.depth > maxDepth) maxDepth = n.depth;
      // Folders first, then files; each by name, so the picture is the same
      // every time it is drawn.
      n.children.sort((a, b) => {
        if (a.group !== b.group) return a.group === "folder" ? -1 : 1;
        return a.name < b.name ? -1 : a.name > b.name ? 1 : 0;
      });
    };
    roll(root);

    graphTree = { root, byId, maxDepth };
    graphDepth = autoGraphDepth();
  }

  /** @param {number} depth */
  function countGraphVisible(depth) {
    let n = 0;
    const walk = (node) => {
      if (node.depth > depth) return;
      if (node.group === "file" && !graphShowFiles) return;
      n++;
      for (const c of node.children) walk(c);
    };
    walk(graphTree.root);
    return n;
  }

  /** The most nesting that still draws a picture rather than a cloud. */
  function autoGraphDepth() {
    if (!graphTree) return 3;
    let best = 1;
    for (let d = 1; d <= graphTree.maxDepth; d++) {
      if (countGraphVisible(d) > GRAPH_VISIBLE_CAP) break;
      best = d;
    }
    return Math.max(1, Math.min(graphTree.maxDepth, best));
  }

  /* ---- the layout ----------------------------------------------------------- */

  /**
   * A radial tree: the workspace at the centre, one ring per level of nesting,
   * every subtree its own wedge. Deterministic — no simulation to settle, so a
   * thousand files draw as fast as ten and land in the same place twice.
   * @param {boolean} refit
   */
  function layoutGraph(refit) {
    if (!graphTree) {
      graphLayout = null;
      syncGraphControls();
      return;
    }
    if (graphDepth > graphTree.maxDepth) graphDepth = graphTree.maxDepth;

    const visible = [];
    const byId = new Map();
    const pick = (node) => {
      if (node.group === "file" && !graphShowFiles) return null;
      const shown = {
        id: node.id,
        name: node.name,
        group: node.group,
        depth: node.depth,
        meta: node.meta,
        symbols: node.group === "file" ? node.symbols : node.subSymbols,
        files: node.subFiles,
        folded: 0,
        children: [],
        leaves: 1,
        angle: 0,
        radius: 0,
        x: 0,
        y: 0,
        r: 4,
        outW: 0,
        inW: 0,
        neighbours: new Map(),
      };
      if (node.depth < graphDepth) {
        for (const c of node.children) {
          const kid = pick(c);
          if (kid) shown.children.push(kid);
        }
      }
      if (!shown.children.length && node.children.length) {
        // The subtree stops here: say how much of it is folded away.
        shown.folded = node.subFiles - (node.group === "file" ? 1 : 0);
      }
      shown.leaves = shown.children.length
        ? shown.children.reduce((a, c) => a + c.leaves, 0)
        : 1;
      visible.push(shown);
      byId.set(shown.id, shown);
      return shown;
    };
    const root = pick(graphTree.root);

    // Angles by leaf count, so a wide subtree gets a wide wedge; radius by
    // depth, spread so the outermost ring has room for its leaves.
    const leaves = Math.max(1, root.leaves);
    const depthSpan = Math.max(1, graphDepth);
    const ringGap = Math.max(
      GRAPH_RING_MIN,
      Math.min(GRAPH_RING_MAX, (leaves * 17) / (2 * Math.PI * depthSpan))
    );
    const place = (node, a0, a1) => {
      node.angle = (a0 + a1) / 2;
      node.span = a1 - a0;
      node.radius = node.depth * ringGap;
      node.x = Math.cos(node.angle) * node.radius;
      node.y = Math.sin(node.angle) * node.radius;
      node.r =
        node.group === "file"
          ? Math.min(12, 3.4 + Math.sqrt(node.symbols) * 0.7)
          : node.group === "root"
            ? 16
            : Math.min(18, 5.5 + Math.sqrt(node.files + 1) * 1.7);
      let a = a0;
      for (const c of node.children) {
        const span = ((a1 - a0) * c.leaves) / Math.max(1, node.leaves);
        place(c, a, a + span);
        a += span;
      }
    };
    // A hair short of a full turn: the first and last wedge stay apart.
    place(root, -Math.PI / 2, -Math.PI / 2 + Math.PI * 2 * 0.997);

    // Links: containment from the tree, relations from the graph, both folded
    // onto whichever ancestor is actually drawn.
    const links = [];
    const index = new Map(visible.map((n, i) => [n.id, i]));
    for (const n of visible) {
      for (const c of n.children) {
        links.push({ a: index.get(n.id), b: index.get(c.id), rel: "in_folder", w: 1 });
      }
    }
    const visibleAncestor = (id) => {
      let node = graphTree.byId.get(id);
      while (node && !byId.has(node.id)) node = node.parent;
      return node ? node.id : null;
    };
    const weights = new Map();
    for (const l of graphData ? graphData.links : []) {
      if (l.relation === "in_folder") continue;
      const a = visibleAncestor(l.source);
      const b = visibleAncestor(l.target);
      if (a === null || b === null || a === b) continue;
      const key = a + "\u0000" + b;
      weights.set(key, (weights.get(key) || 0) + (Number(l.weight) || 1));
    }
    const relations = [];
    for (const [key, w] of weights) {
      const [a, b] = key.split("\u0000");
      const ia = index.get(a);
      const ib = index.get(b);
      if (ia === undefined || ib === undefined) continue;
      relations.push({ a: ia, b: ib, rel: "calls", w });
      const na = visible[ia];
      const nb = visible[ib];
      na.outW += w;
      nb.inW += w;
      na.neighbours.set(b, (na.neighbours.get(b) || 0) + w);
      nb.neighbours.set(a, (nb.neighbours.get(a) || 0) + w);
    }
    // Every relation counts towards a node's own numbers and its list of
    // neighbours; only the heaviest are drawn, or the picture is a haze.
    relations.sort((x, y) => y.w - x.w);
    for (const e of relations.slice(0, GRAPH_LINK_CAP)) links.push(e);
    const relationsDrawn = Math.min(relations.length, GRAPH_LINK_CAP);
    const relationsTotal = relations.length;

    const keep = graphLayout && !refit ? graphLayout : null;
    graphLayout = {
      nodes: visible,
      links,
      byId,
      index,
      relationsDrawn,
      relationsTotal,
      ringGap,
      maxRadius: depthSpan * ringGap,
      scale: keep ? keep.scale : 1,
      tx: keep ? keep.tx : 0,
      ty: keep ? keep.ty : 0,
      fitPending: !keep,
    };
    graphHover = null;
    syncGraphControls();
  }

  function graphCssVar(name, fallback) {
    try {
      const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
      return v || fallback;
    } catch (e) {
      return fallback;
    }
  }

  function fitGraphToView(width, height) {
    const s = graphLayout;
    if (!s || !s.nodes.length) return;
    let reach = 1;
    for (const n of s.nodes) reach = Math.max(reach, n.radius + n.r);
    const span = reach * 2 + 60;
    s.scale = Math.max(0.04, Math.min(2.5, Math.min((width - 32) / span, (height - 32) / span)));
    s.tx = width / 2;
    s.ty = height / 2;
  }

  function scheduleGraphDraw() {
    if (graphDrawQueued) return;
    graphDrawQueued = true;
    requestAnimationFrame(() => {
      graphDrawQueued = false;
      drawGraph();
    });
  }

  function drawGraph() {
    if (!graphViewActive() || !graphCanvas || !graphCanvas.getContext) return;
    const rect = graphCanvas.getBoundingClientRect();
    const width = Math.max(1, Math.floor(rect.width));
    const height = Math.max(1, Math.floor(rect.height));
    const dpr = window.devicePixelRatio || 1;
    if (graphCanvas.width !== Math.floor(width * dpr) || graphCanvas.height !== Math.floor(height * dpr)) {
      graphCanvas.width = Math.floor(width * dpr);
      graphCanvas.height = Math.floor(height * dpr);
    }
    const ctx = graphCanvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);
    const s = graphLayout;
    if (!s) return;
    if (s.fitPending) {
      fitGraphToView(width, height);
      s.fitPending = false;
    }

    const fg = graphCssVar("--fg", "#e6e6ea");
    const muted = graphCssVar("--muted", "#86868d");
    const accent = graphCssVar("--accent", "#8b8cff");
    const border = graphCssVar("--border", "#2b2b30");
    const surface = graphCssVar("--surface", "#1f1f23");
    const focus = graphSelectedId ? s.byId.get(graphSelectedId) : null;
    const hot = graphHover || focus;

    ctx.save();
    ctx.translate(s.tx, s.ty);
    ctx.scale(s.scale, s.scale);
    const inv = 1 / s.scale;

    // The rings themselves: one per level of nesting that has anything on it.
    let deepest = 0;
    for (const n of s.nodes) deepest = Math.max(deepest, n.depth);
    ctx.strokeStyle = border;
    ctx.globalAlpha = 0.55;
    for (let d = 1; d <= deepest; d++) {
      ctx.beginPath();
      ctx.arc(0, 0, d * s.ringGap, 0, Math.PI * 2);
      ctx.lineWidth = inv;
      ctx.stroke();
    }
    ctx.globalAlpha = 1;

    // Containment, drawn as the tree it is: out along the parent's ring to
    // the child's angle, then outwards to the child.
    ctx.lineCap = "round";
    for (const e of s.links) {
      if (e.rel !== "in_folder") continue;
      const a = s.nodes[e.a];
      const b = s.nodes[e.b];
      const lit = hot === a || hot === b;
      ctx.strokeStyle = lit ? muted : border;
      ctx.globalAlpha = lit ? 1 : 0.8;
      ctx.lineWidth = (lit ? 1.6 : 1) * inv;
      ctx.beginPath();
      ctx.moveTo(a.x, a.y);
      ctx.quadraticCurveTo(Math.cos(b.angle) * a.radius, Math.sin(b.angle) * a.radius, b.x, b.y);
      ctx.stroke();
    }

    // Relations, bowed towards the middle so a bundle of them reads as one
    // stream rather than a net over the whole picture.
    for (const e of s.links) {
      if (e.rel === "in_folder") continue;
      const a = s.nodes[e.a];
      const b = s.nodes[e.b];
      const lit = hot === a || hot === b;
      if (!graphShowLinks && !lit) continue;
      ctx.strokeStyle = lit ? fg : accent;
      ctx.globalAlpha = lit ? 0.95 : hot ? 0.05 : 0.13;
      ctx.lineWidth = Math.min(4, 0.7 + Math.log(e.w + 1) * 0.6) * inv;
      ctx.beginPath();
      ctx.moveTo(a.x, a.y);
      ctx.quadraticCurveTo(((a.x + b.x) / 2) * 0.35, ((a.y + b.y) / 2) * 0.35, b.x, b.y);
      ctx.stroke();
    }
    ctx.globalAlpha = 1;

    for (const a of s.nodes) {
      const lit = hot === a;
      const near = hot && hot.neighbours && hot.neighbours.has(a.id);
      ctx.beginPath();
      ctx.arc(a.x, a.y, a.r, 0, Math.PI * 2);
      if (a.group === "file") {
        ctx.fillStyle = lit ? fg : graphColorFor(a.id) || accent;
        ctx.globalAlpha = hot && !lit && !near ? 0.55 : 1;
        ctx.fill();
      } else {
        ctx.fillStyle = surface;
        ctx.fill();
        ctx.lineWidth = (lit ? 2.4 : 1.5) * inv;
        ctx.strokeStyle = lit ? fg : a.group === "root" ? accent : muted;
        ctx.stroke();
      }
      ctx.globalAlpha = 1;
      if (graphSelectedId && a.id === graphSelectedId) {
        ctx.beginPath();
        ctx.arc(a.x, a.y, a.r + 5 * inv, 0, Math.PI * 2);
        ctx.strokeStyle = accent;
        ctx.lineWidth = 2 * inv;
        ctx.stroke();
      }
    }

    // Labels at screen size, turned to sit along their ring: folders whenever
    // there are few enough to read, files when the view is close enough or the
    // node is the one being looked at. The name is the point of the picture.
    ctx.textBaseline = "middle";
    for (const a of s.nodes) {
      const lit = hot === a;
      // A name is drawn when its own slice of the ring is wide enough on
      // screen to hold one, which is what keeps a thousand files from
      // writing over each other; the one being looked at always is.
      const room = a.span * Math.max(a.radius, s.ringGap) * s.scale;
      const named = hot && hot.neighbours && hot.neighbours.size <= 40 && hot.neighbours.has(a.id);
      const show = a.group === "root" || lit || named || room > (a.group === "folder" ? 12 : 13);
      if (!show) continue;
      const size = (a.group === "root" ? 14 : a.group === "folder" ? 12 : 11) * inv;
      ctx.font = size + "px ui-sans-serif, system-ui, sans-serif";
      ctx.fillStyle = lit || a.group === "root" ? fg : muted;
      ctx.globalAlpha = lit ? 1 : 0.9;
      if (a.group === "root") {
        ctx.textAlign = "center";
        ctx.fillText(a.name, 0, -a.r - 10 * inv);
        ctx.textAlign = "left";
        continue;
      }
      // Along the ray, reading outwards; flipped on the left half so no name
      // is upside down.
      const flip = Math.cos(a.angle) < 0;
      ctx.save();
      ctx.translate(a.x, a.y);
      ctx.rotate(a.angle + (flip ? Math.PI : 0));
      ctx.textAlign = flip ? "right" : "left";
      ctx.fillText(a.name, (flip ? -1 : 1) * (a.r + 5 * inv), 0);
      ctx.restore();
    }
    ctx.globalAlpha = 1;
    ctx.textAlign = "left";
    ctx.restore();
  }

  /* ---- the pointer ---------------------------------------------------------- */

  /** World coordinates of a pointer event on the canvas. */
  function graphWorldPoint(ev) {
    const rect = graphCanvas.getBoundingClientRect();
    const sx = ev.clientX - rect.left;
    const sy = ev.clientY - rect.top;
    const s = graphLayout;
    return { sx, sy, x: (sx - s.tx) / s.scale, y: (sy - s.ty) / s.scale };
  }

  function graphNodeAt(x, y) {
    const s = graphLayout;
    if (!s) return null;
    let best = null;
    let bestD = Infinity;
    const slack = 5 / s.scale;
    for (const a of s.nodes) {
      const dx = a.x - x;
      const dy = a.y - y;
      const d = Math.sqrt(dx * dx + dy * dy);
      if (d <= a.r + slack && d < bestD) {
        best = a;
        bestD = d;
      }
    }
    return best;
  }

  function showGraphCard(node, sx, sy) {
    if (!graphCardEl) return;
    if (!node) {
      graphCardEl.hidden = true;
      return;
    }
    // textContent throughout: names and paths come off the disk.
    graphCardEl.innerHTML = "";
    const title = document.createElement("div");
    title.className = "graph-card-title";
    title.textContent = node.name;
    const path = document.createElement("div");
    path.className = "graph-card-path";
    path.textContent = node.id || "(workspace root)";
    graphCardEl.append(title, path);
    const rows = [];
    if (node.group === "file") {
      rows.push(["file", node.symbols ? node.symbols + " symbols" : ""]);
    } else {
      rows.push([node.group === "root" ? "workspace" : "folder", node.files + " files"]);
      if (node.folded) rows.push(["folded in", node.folded + " files deeper"]);
    }
    if (node.outW || node.inW) rows.push(["links", node.outW + " out · " + node.inW + " in"]);
    for (const [k, v] of rows) {
      if (!v) continue;
      const row = document.createElement("div");
      row.className = "graph-card-row";
      const key = document.createElement("span");
      key.textContent = k;
      const val = document.createElement("span");
      val.textContent = v;
      row.append(key, val);
      graphCardEl.appendChild(row);
    }
    graphCardEl.hidden = false;
    const stage = graphStage.getBoundingClientRect();
    const canvasRect = graphCanvas.getBoundingClientRect();
    let left = canvasRect.left - stage.left + sx + 14;
    let top = canvasRect.top - stage.top + sy + 14;
    const cw = graphCardEl.offsetWidth || 220;
    const ch = graphCardEl.offsetHeight || 90;
    if (left + cw > stage.width - 8) left = Math.max(8, left - cw - 28);
    if (top + ch > stage.height - 8) top = Math.max(8, top - ch - 28);
    graphCardEl.style.left = left + "px";
    graphCardEl.style.top = top + "px";
  }

  function bindGraphCanvas() {
    if (!graphCanvas || !graphCanvas.addEventListener) return;
    graphCanvas.addEventListener("pointerdown", (ev) => {
      if (!graphLayout) return;
      const p = graphWorldPoint(ev);
      graphDrag = { lastX: p.sx, lastY: p.sy, moved: 0, node: graphNodeAt(p.x, p.y) };
      if (graphCanvas.setPointerCapture) graphCanvas.setPointerCapture(ev.pointerId);
    });
    graphCanvas.addEventListener("pointermove", (ev) => {
      if (!graphLayout) return;
      const p = graphWorldPoint(ev);
      if (graphDrag) {
        const dx = p.sx - graphDrag.lastX;
        const dy = p.sy - graphDrag.lastY;
        graphDrag.lastX = p.sx;
        graphDrag.lastY = p.sy;
        graphDrag.moved += Math.abs(dx) + Math.abs(dy);
        if (graphDrag.moved > 3) {
          graphLayout.tx += dx;
          graphLayout.ty += dy;
          graphCanvas.classList.add("dragging");
          showGraphCard(null);
          scheduleGraphDraw();
        }
        return;
      }
      const node = graphNodeAt(p.x, p.y);
      if (node !== graphHover) {
        graphHover = node;
        scheduleGraphDraw();
      }
      showGraphCard(node, p.sx, p.sy);
      graphCanvas.classList.toggle("over-node", Boolean(node));
    });
    const release = (ev) => {
      const drag = graphDrag;
      graphDrag = null;
      graphCanvas.classList.remove("dragging");
      if (drag && drag.moved <= 3 && ev.type === "pointerup") {
        selectGraphNode(drag.node ? drag.node.id : "");
      }
    };
    graphCanvas.addEventListener("pointerup", release);
    graphCanvas.addEventListener("pointercancel", release);
    graphCanvas.addEventListener("pointerleave", () => {
      graphHover = null;
      showGraphCard(null);
      scheduleGraphDraw();
    });
    graphCanvas.addEventListener(
      "wheel",
      (ev) => {
        if (!graphLayout) return;
        ev.preventDefault();
        const p = graphWorldPoint(ev);
        const factor = Math.exp(-ev.deltaY * 0.0012);
        const next = Math.max(0.03, Math.min(8, graphLayout.scale * factor));
        // Zoom about the pointer: the world point under it stays put.
        graphLayout.tx = p.sx - p.x * next;
        graphLayout.ty = p.sy - p.y * next;
        graphLayout.scale = next;
        graphLayout.fitPending = false;
        scheduleGraphDraw();
      },
      { passive: false }
    );
    graphCanvas.addEventListener("dblclick", () => {
      if (graphLayout) {
        graphLayout.fitPending = true;
        scheduleGraphDraw();
      }
    });
  }

  /* ---- the readout ---------------------------------------------------------- */

  /** @param {string} id */
  function selectGraphNode(id) {
    const node = graphLayout ? graphLayout.byId.get(id) : null;
    graphSelectedId = node ? id : "";
    graphOpenSymbol = -1;
    graphOutline = null;
    renderGraphSide();
    scheduleGraphDraw();
    if (node && node.group === "file") {
      void loadGraphOutline(node.id);
    }
  }

  /** @param {string} path */
  async function loadGraphOutline(path) {
    const projectId = currentProjectId;
    const conn = projectId ? connFor(projectId) : null;
    if (!conn || !conn.isOpen()) return;
    const seq = ++graphOutlineSeq;
    graphOutline = { path, loading: true, error: "", result: null };
    renderGraphSide();
    try {
      const r = await conn.send("index.outline", { path });
      if (seq !== graphOutlineSeq || projectId !== currentProjectId) return;
      graphOutline = { path, loading: false, error: "", result: r || {} };
    } catch (err) {
      if (seq !== graphOutlineSeq) return;
      graphOutline = { path, loading: false, error: String((err && err.message) || err), result: null };
    }
    renderGraphSide();
  }

  /** @param {any} parent @param {string} cls @param {string} text */
  function graphEl(parent, cls, text) {
    const el = document.createElement("div");
    el.className = cls;
    if (text !== undefined) el.textContent = text;
    if (parent) parent.appendChild(el);
    return el;
  }

  /** A label, a leader, a value — the shape a console gives a count. */
  function graphReadout(parent, label, value, extraClass) {
    const row = document.createElement("div");
    row.className = "graph-ro" + (extraClass ? " " + extraClass : "");
    const k = document.createElement("span");
    k.className = "graph-ro-k";
    k.textContent = label;
    const dots = document.createElement("span");
    dots.className = "graph-ro-dots";
    const v = document.createElement("span");
    v.className = "graph-ro-v";
    v.textContent = String(value);
    row.append(k, dots, v);
    parent.appendChild(row);
    return row;
  }

  function graphSection(parent, title) {
    const block = document.createElement("section");
    block.className = "graph-block";
    graphEl(block, "graph-block-title", title);
    parent.appendChild(block);
    return block;
  }

  function renderGraphSide() {
    if (!graphSideEl) return;
    graphSideEl.innerHTML = "";
    const stats = (graphData && graphData.stats) || {};
    const nodes = (graphData && graphData.nodes) || [];

    const head = graphSection(graphSideEl, "Workspace");
    graphEl(head, "graph-head-name", graphProjectName());
    const entry = typeof known !== "undefined" ? known.find((p) => p.id === currentProjectId) : null;
    if (entry && entry.path) graphEl(head, "graph-head-path", entry.path);
    graphReadout(head, "index", graphData ? (graphData.available ? "ready" : "empty") : "—",
      graphData && graphData.available ? "ok" : "");

    // Whatever is selected goes straight under the workspace: it is what the
    // person just clicked, and the counters are not going anywhere.
    if (graphSelectedId) renderGraphSelection(graphSideEl);

    if (graphData) {
      const folders = nodes.filter((n) => n.group === "folder").length;
      const files = nodes.filter((n) => n.group === "file").length;
      const relations = (graphData.links || []).filter((l) => l.relation !== "in_folder").length;
      const index = graphSection(graphSideEl, "What is indexed");
      graphReadout(index, "files", stats.files || files);
      graphReadout(index, "folders", folders);
      graphReadout(index, "symbols", stats.nodes || 0);
      graphReadout(index, "functions", stats.funcs || 0);
      graphReadout(index, "types", stats.types || 0);
      graphReadout(index, "tests", stats.tests || 0);
      graphReadout(index, "packages", stats.packages || 0);
      graphReadout(index, "relations", stats.edges || 0);
      graphReadout(index, "file links", relations);
      if (stats.embeddings) {
        graphReadout(index, "embeddings", stats.embeddings + (stats.missing_embeddings ? " (+" + stats.missing_embeddings + " missing)" : ""));
      }
      graphReadout(index, "nesting", graphTree ? graphTree.maxDepth + " levels" : "—");

      // What the files are made of: the graph's own languages when it has
      // them, the extensions of the file nodes otherwise.
      const kinds = new Map();
      const langs = stats.langs && typeof stats.langs === "object" ? stats.langs : null;
      if (langs) {
        for (const k of Object.keys(langs)) kinds.set(k, langs[k]);
      } else {
        for (const n of nodes) {
          if (n.group !== "file") continue;
          const ext = graphExtOf(n.id) || "other";
          kinds.set(ext, (kinds.get(ext) || 0) + 1);
        }
      }
      const sorted = [...kinds.entries()].sort((a, b) => b[1] - a[1]).slice(0, 10);
      if (sorted.length) {
        const block = graphSection(graphSideEl, "File types");
        for (const [name, count] of sorted) {
          const row = graphReadout(block, name, count, "graph-ro-lang");
          const dot = document.createElement("i");
          dot.className = "graph-swatch";
          dot.style.background = GRAPH_LANG_COLORS[String(name).toLowerCase()] || "var(--accent)";
          row.insertBefore(dot, row.firstChild);
        }
      }

      // The files everything else leans on: the picture's centre of gravity.
      if (graphLayout) {
        const hubs = graphLayout.nodes
          .filter((n) => n.group === "file" && n.inW + n.outW > 0)
          .sort((a, b) => b.inW + b.outW - (a.inW + a.outW))
          .slice(0, 6);
        if (hubs.length) {
          const block = graphSection(graphSideEl, "Most connected");
          for (const h of hubs) graphNeighbourRow(block, h.id, h.inW + h.outW);
        }
      }
    }

    if (!graphSelectedId) renderGraphSelection(graphSideEl);
  }

  /** @param {any} parent @param {string} id @param {number} weight */
  function graphNeighbourRow(parent, id, weight) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "graph-link-row";
    const name = document.createElement("span");
    name.className = "graph-link-name";
    name.textContent = id.slice(id.lastIndexOf("/") + 1);
    const path = document.createElement("span");
    path.className = "graph-link-path";
    path.textContent = id;
    const w = document.createElement("span");
    w.className = "graph-link-weight";
    w.textContent = String(weight);
    // Name and weight share the first row; the path runs under both.
    row.append(name, w, path);
    row.title = id;
    row.addEventListener("click", () => selectGraphNode(id));
    parent.appendChild(row);
    return row;
  }

  const GRAPH_SYMBOL_LABELS = {
    func: "fn",
    method: "fn",
    struct: "type",
    interface: "iface",
    type: "type",
    test: "test",
    const: "const",
    var: "var",
  };

  /** @param {any} parent */
  function renderGraphSelection(parent) {
    const node = graphLayout && graphSelectedId ? graphLayout.byId.get(graphSelectedId) : null;
    if (!node) {
      const empty = graphSection(parent, "Selection");
      graphEl(empty, "graph-empty", "Click a node to see what is inside it and what it is wired to.");
      return;
    }
    const block = graphSection(parent, node.group === "file" ? "File" : "Folder");
    graphEl(block, "graph-head-name", node.name);
    graphEl(block, "graph-head-path", node.id || "(workspace root)");
    if (node.group === "file") {
      graphReadout(block, "symbols", node.symbols);
      const ext = graphExtOf(node.id);
      if (ext) graphReadout(block, "type", ext);
    } else {
      graphReadout(block, "files", node.files);
      graphReadout(block, "symbols", node.symbols);
      if (node.folded) graphReadout(block, "folded away", node.folded + " files");
    }
    graphReadout(block, "links out", node.outW);
    graphReadout(block, "links in", node.inW);

    // For a file the functions come first — that is what the person opened it
    // for; its neighbours follow.
    if (node.group === "file") renderGraphFunctions(parent, node);
    const neighbours = [...node.neighbours.entries()].sort((a, b) => b[1] - a[1]).slice(0, 10);
    if (neighbours.length) {
      const nb = graphSection(parent, "Wired to");
      for (const [id, w] of neighbours) graphNeighbourRow(nb, id, w);
    }
  }

  /** @param {any} parent @param {any} node */
  function renderGraphFunctions(parent, node) {
    const fns = graphSection(parent, "Inside this file");
    if (!graphOutline || graphOutline.path !== node.id) {
      graphEl(fns, "graph-empty", "Reading…");
      return;
    }
    if (graphOutline.loading) {
      graphEl(fns, "graph-empty", "Reading…");
      return;
    }
    if (graphOutline.error) {
      graphEl(fns, "graph-empty", graphOutline.error);
      return;
    }
    const res = graphOutline.result || {};
    const symbols = Array.isArray(res.symbols) ? res.symbols : [];
    if (res.lines) {
      graphReadout(fns, "lines", res.lines);
    }
    if (!symbols.length) {
      graphEl(fns, "graph-empty", res.available ? "No symbols indexed in this file." : "This file is not in the index.");
      return;
    }
    symbols.forEach((sym, i) => {
      const row = document.createElement("button");
      row.type = "button";
      row.className = "graph-sym" + (graphOpenSymbol === i ? " open" : "");
      const kind = document.createElement("span");
      kind.className = "graph-sym-kind";
      kind.textContent = GRAPH_SYMBOL_LABELS[String(sym.kind || "").toLowerCase()] || String(sym.kind || "sym");
      const name = document.createElement("span");
      name.className = "graph-sym-name";
      name.textContent = sym.name || "(unnamed)";
      const where = document.createElement("span");
      where.className = "graph-sym-lines";
      where.textContent = sym.line_start ? sym.line_start + "–" + sym.line_end : "";
      row.append(kind, name, where);
      row.title = (sym.fqn || sym.name || "") + " · " + (sym.calls_out || 0) + " out · " + (sym.calls_in || 0) + " in";
      row.addEventListener("click", () => {
        graphOpenSymbol = graphOpenSymbol === i ? -1 : i;
        renderGraphSide();
      });
      fns.appendChild(row);
      if (graphOpenSymbol === i) {
        const pre = document.createElement("pre");
        pre.className = "graph-code";
        // textContent: this is source off the disk, never markup.
        pre.textContent = sym.preview || "(no source to show)";
        fns.appendChild(pre);
        if (sym.truncated) {
          const shown = (sym.preview || "").split("\n").length;
          const whole = Number(sym.line_end) - Number(sym.line_start) + 1;
          graphEl(fns, "graph-code-note", "first " + shown + " of " + whole + " lines");
        }
      }
    });
  }

  ensureGraphView();
