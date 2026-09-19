// The element picker, injected into every page the Orchestra browser panel
// opens (Tauri `initialization_script`, so it runs before the page's own
// scripts and survives navigation).
//
// It talks to nobody. Arming and collecting are both driven from Rust with
// `eval` / `eval_with_callback`, which is the whole security design: a page we
// do not control shares this world, so it must not be handed a token, a core
// address or a Tauri IPC bridge. The worst a hostile page can do here is lie
// about what was picked, and the user sees the chip before sending it.
(function () {
  if (globalThis.__orchPick) return;

  var MAX_DEPTH = 3; // levels of children below the picked node
  var MAX_TEXT = 200; // characters of one text node or attribute
  var MAX_MARKUP = 20000; // characters of the whole subtree
  var MAX_RULES = 40;
  var VOID = { area: 1, base: 1, br: 1, col: 1, embed: 1, hr: 1, img: 1, input: 1, link: 1, meta: 1, source: 1, track: 1, wbr: 1 };

  var armed = false;
  var pending = null;
  var boxEl = null;
  var tagEl = null;
  var hovered = null;

  function isEl(n) { return !!n && n.nodeType === 1; }

  function esc(s) {
    s = String(s);
    if (globalThis.CSS && typeof globalThis.CSS.escape === "function") return globalThis.CSS.escape(s);
    return s.replace(/[^a-zA-Z0-9_-]/g, function (c) { return "\\" + c; });
  }

  function classesOf(el) {
    var raw = el.getAttribute && el.getAttribute("class");
    if (!raw) return [];
    return String(raw).split(/\s+/).filter(Boolean);
  }

  function siblingIndex(el) {
    var parent = el.parentElement;
    if (!parent || !parent.children) return 0;
    var same = [];
    for (var i = 0; i < parent.children.length; i++) {
      if (parent.children[i].tagName === el.tagName) same.push(parent.children[i]);
    }
    return same.length > 1 ? same.indexOf(el) + 1 : 0;
  }

  // A selector that finds this element again. A unique id wins; otherwise a
  // short chain of tag plus up to two classes, with :nth-of-type only where
  // the tag repeats among its siblings.
  function selectorFor(el) {
    if (!isEl(el)) return "";
    var doc = el.ownerDocument || globalThis.document;
    if (el.id && doc && doc.querySelectorAll && doc.querySelectorAll("#" + esc(el.id)).length === 1) {
      return "#" + esc(el.id);
    }
    var parts = [];
    var cur = el;
    while (isEl(cur) && parts.length < 6) {
      if (cur.id) { parts.unshift("#" + esc(cur.id)); break; }
      var part = cur.tagName.toLowerCase();
      var cls = classesOf(cur).slice(0, 2);
      for (var i = 0; i < cls.length; i++) part += "." + esc(cls[i]);
      var n = siblingIndex(cur);
      if (n) part += ":nth-of-type(" + n + ")";
      parts.unshift(part);
      cur = cur.parentElement;
      if (!cur || cur.tagName === "HTML") break;
    }
    return parts.join(" > ");
  }

  function attrText(el) {
    var out = "";
    var attrs = el.attributes || [];
    for (var i = 0; i < attrs.length; i++) {
      var a = attrs[i];
      if (!a || !a.name || a.name === "data-orch-pick") continue;
      var v = String(a.value == null ? "" : a.value);
      if (v.length > MAX_TEXT) v = v.slice(0, MAX_TEXT) + "…";
      out += " " + a.name + (v ? "=\"" + v.replace(/"/g, "&quot;") + "\"" : "");
    }
    return out;
  }

  function textOf(el) {
    var out = "";
    var kids = el.childNodes || [];
    for (var i = 0; i < kids.length; i++) {
      if (kids[i].nodeType === 3) out += kids[i].nodeValue || "";
    }
    out = out.replace(/\s+/g, " ").trim();
    return out.length > MAX_TEXT ? out.slice(0, MAX_TEXT) + "…" : out;
  }

  // The rendered subtree, cut to a readable size. A screen-sized block is
  // routinely 100 KB of markup and this text is re-read on every turn it stays
  // attached to the conversation.
  function markup(el, depth, indent) {
    if (!isEl(el)) return "";
    depth = depth == null ? MAX_DEPTH : depth;
    indent = indent || "";
    var name = el.tagName.toLowerCase();
    if (VOID[name]) return indent + "<" + name + attrText(el) + " />";
    var open = indent + "<" + name + attrText(el) + ">";
    var kids = [];
    var children = el.children || [];
    for (var i = 0; i < children.length; i++) kids.push(children[i]);
    var own = textOf(el);
    if (!kids.length) return open + own + "</" + name + ">";
    if (depth <= 0) {
      return open + "\n" + indent + "  <!-- " + kids.length + " more element(s) -->\n" + indent + "</" + name + ">";
    }
    var body = "";
    if (own) body += "\n" + indent + "  " + own;
    for (var j = 0; j < kids.length; j++) {
      body += "\n" + markup(kids[j], depth - 1, indent + "  ");
      if (body.length > MAX_MARKUP) { body += "\n" + indent + "  <!-- truncated -->"; break; }
    }
    return open + body + "\n" + indent + "</" + name + ">";
  }

  // The rules that actually match, not getComputedStyle: 340 resolved
  // properties per node are noise, and what the model needs is the CSS someone
  // wrote. Cross-origin sheets throw on .cssRules and are skipped.
  function appliedRules(el) {
    var doc = el.ownerDocument || globalThis.document;
    var sheets = (doc && doc.styleSheets) || [];
    var out = [];
    for (var i = 0; i < sheets.length && out.length < MAX_RULES; i++) {
      var rules;
      try { rules = sheets[i].cssRules; } catch (e) { continue; }
      if (!rules) continue;
      for (var j = 0; j < rules.length && out.length < MAX_RULES; j++) {
        var r = rules[j];
        if (!r || !r.selectorText || !r.cssText) continue;
        var hit = false;
        try { hit = !!(el.matches && el.matches(r.selectorText)); } catch (e2) { hit = false; }
        if (hit) out.push(r.cssText);
      }
    }
    var inline = el.getAttribute && el.getAttribute("style");
    if (inline) out.unshift("[inline] { " + inline + " }");
    return out;
  }

  function rectOf(el) {
    if (!el.getBoundingClientRect) return null;
    var r = el.getBoundingClientRect();
    return { x: Math.round(r.left), y: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height) };
  }

  function payloadFor(el) {
    var doc = el.ownerDocument || globalThis.document;
    return {
      url: (globalThis.location && globalThis.location.href) || "",
      title: (doc && doc.title) || "",
      tag: el.tagName.toLowerCase(),
      selector: selectorFor(el),
      markup: markup(el, MAX_DEPTH, ""),
      css: appliedRules(el),
      box: rectOf(el),
    };
  }

  /* ---- overlay ---------------------------------------------------------- */

  function ensureOverlay() {
    var doc = globalThis.document;
    if (!doc || !doc.createElement) return;
    if (!boxEl) {
      boxEl = doc.createElement("div");
      boxEl.setAttribute("data-orch-pick", "box");
      boxEl.style.cssText = "position:fixed;z-index:2147483647;pointer-events:none;border:2px solid #4c8bf5;background:rgba(76,139,245,.12);border-radius:2px";
      tagEl = doc.createElement("div");
      tagEl.setAttribute("data-orch-pick", "tag");
      tagEl.style.cssText = "position:fixed;z-index:2147483647;pointer-events:none;font:11px/1.6 ui-monospace,Menlo,Consolas,monospace;background:#4c8bf5;color:#fff;padding:0 6px;border-radius:2px;white-space:nowrap";
    }
    var host = doc.body || doc.documentElement;
    if (host && host.appendChild && boxEl.parentNode !== host) {
      host.appendChild(boxEl);
      host.appendChild(tagEl);
    }
  }

  function moveOverlay(el) {
    if (!boxEl) return;
    var r = rectOf(el);
    if (!r) return;
    boxEl.style.left = r.x + "px";
    boxEl.style.top = r.y + "px";
    boxEl.style.width = r.w + "px";
    boxEl.style.height = r.h + "px";
    var label = el.tagName.toLowerCase();
    var cls = classesOf(el)[0];
    if (cls) label += "." + cls;
    tagEl.textContent = label + "  " + r.w + "×" + r.h;
    tagEl.style.left = r.x + "px";
    tagEl.style.top = (r.y > 20 ? r.y - 20 : r.y + r.h) + "px";
  }

  function hideOverlay() {
    if (boxEl && boxEl.parentNode) boxEl.parentNode.removeChild(boxEl);
    if (tagEl && tagEl.parentNode) tagEl.parentNode.removeChild(tagEl);
  }

  function targetOf(ev) {
    var el = ev && (ev.target || ev.srcElement);
    if (isEl(el) && el.getAttribute && el.getAttribute("data-orch-pick")) return null;
    return isEl(el) ? el : null;
  }

  function onMove(ev) {
    if (!armed) return;
    var el = targetOf(ev);
    if (!el || el === hovered) return;
    hovered = el;
    moveOverlay(el);
  }

  function onClick(ev) {
    if (!armed) return;
    var el = targetOf(ev) || hovered;
    if (ev && ev.preventDefault) ev.preventDefault();
    if (ev && ev.stopPropagation) ev.stopPropagation();
    if (el) {
      try { pending = payloadFor(el); } catch (e) { pending = { error: String(e) }; }
    }
    disarm();
  }

  function onKey(ev) {
    if (armed && ev && (ev.key === "Escape" || ev.keyCode === 27)) disarm();
  }

  function listen(on) {
    var doc = globalThis.document;
    if (!doc || !doc.addEventListener) return;
    var fn = on ? "addEventListener" : "removeEventListener";
    doc[fn]("mousemove", onMove, true);
    doc[fn]("click", onClick, true);
    doc[fn]("keydown", onKey, true);
  }

  function arm() {
    if (armed) return "armed";
    armed = true;
    hovered = null;
    ensureOverlay();
    listen(true);
    return "armed";
  }

  function disarm() {
    if (!armed) return "idle";
    armed = false;
    hovered = null;
    listen(false);
    hideOverlay();
    return "idle";
  }

  // Rust polls this while the picker is armed. It returns a string and never
  // throws: eval_with_callback swallows exceptions on Windows.
  function take() {
    try {
      if (!pending) return armed ? "" : "idle";
      var out = JSON.stringify(pending);
      pending = null;
      return out;
    } catch (e) {
      pending = null;
      return JSON.stringify({ error: String(e) });
    }
  }

  /* ---- the console tap -------------------------------------------------- */
  //
  // The page cannot reach us, so what it logs is kept here until the panel
  // asks. A ring buffer: a page in a loop must not grow this without bound.

  var MAX_LOGS = 300;
  var MAX_LOG_TEXT = 2000;
  var logs = [];

  // Any value as one short line, which is all a console row is.
  function fmt(v) {
    try {
      if (typeof v === "string") return v.length > MAX_LOG_TEXT ? v.slice(0, MAX_LOG_TEXT) + "…" : v;
      if (v === undefined) return "undefined";
      if (v === null) return "null";
      if (typeof v === "function") return "function " + (v.name || "");
      if (typeof v !== "object") return String(v);
      if (v instanceof Error) return String(v.name || "Error") + ": " + String(v.message || "");
      if (isEl(v)) return "<" + v.tagName.toLowerCase() + ">";
      var out = JSON.stringify(v);
      if (out === undefined) return String(v);
      return out.length > MAX_LOG_TEXT ? out.slice(0, MAX_LOG_TEXT) + "…" : out;
    } catch (e) {
      try { return String(v); } catch (e2) { return "[unprintable]"; }
    }
  }

  function pushLog(level, args) {
    var parts = [];
    for (var i = 0; i < args.length; i++) parts.push(fmt(args[i]));
    logs.push({ level: level, text: parts.join(" ") });
    if (logs.length > MAX_LOGS) logs.splice(0, logs.length - MAX_LOGS);
  }

  // The page keeps its own console: every call still reaches it, so a site
  // that reads its own logs is not changed by being watched.
  function tapConsole() {
    var c = globalThis.console;
    if (!c || c.__orchTapped) return;
    var levels = ["log", "info", "warn", "error", "debug"];
    for (var i = 0; i < levels.length; i++) {
      (function (level) {
        var original = c[level];
        if (typeof original !== "function") return;
        c[level] = function () {
          try { pushLog(level, arguments); } catch (e) { /* a log is never worth a throw */ }
          return original.apply(c, arguments);
        };
      })(levels[i]);
    }
    c.__orchTapped = true;
    if (typeof globalThis.addEventListener === "function") {
      globalThis.addEventListener("error", function (ev) {
        pushLog("error", [(ev && (ev.message || ev.error)) || "error"]);
      });
      globalThis.addEventListener("unhandledrejection", function (ev) {
        pushLog("error", ["Unhandled rejection: " + fmt(ev && ev.reason)]);
      });
    }
  }

  // Handed over once, like take(): the panel has drawn them.
  function drainLog() {
    var out = logs;
    logs = [];
    return out;
  }

  /* ---- the area to shoot ------------------------------------------------ */
  //
  // Same idea as the picker, a rectangle instead of an element: the panel
  // arms it, the user drags, and the rectangle is collected by asking. Page
  // coordinates, because that is what the screenshot's clip takes.

  var areaArmed = false;
  var areaFrom = null;
  var areaRect = null;
  var areaBox = null;

  function ensureAreaBox() {
    var doc = globalThis.document;
    if (!doc || !doc.createElement) return;
    if (!areaBox) {
      areaBox = doc.createElement("div");
      areaBox.setAttribute("data-orch-pick", "area");
      areaBox.style.cssText = "position:fixed;z-index:2147483647;pointer-events:none;border:1px dashed #4c8bf5;background:rgba(76,139,245,.10)";
    }
    var host = doc.body || doc.documentElement;
    if (host && host.appendChild && areaBox.parentNode !== host) host.appendChild(areaBox);
    areaBox.style.width = "0px";
    areaBox.style.height = "0px";
  }

  function drawArea(a, b) {
    if (!areaBox) return;
    areaBox.style.left = Math.min(a.x, b.x) + "px";
    areaBox.style.top = Math.min(a.y, b.y) + "px";
    areaBox.style.width = Math.abs(a.x - b.x) + "px";
    areaBox.style.height = Math.abs(a.y - b.y) + "px";
  }

  function pointOf(ev) {
    return { x: (ev && ev.clientX) || 0, y: (ev && ev.clientY) || 0 };
  }

  function onAreaDown(ev) {
    if (!areaArmed) return;
    if (ev && ev.preventDefault) ev.preventDefault();
    if (ev && ev.stopPropagation) ev.stopPropagation();
    areaFrom = pointOf(ev);
    drawArea(areaFrom, areaFrom);
  }

  function onAreaMove(ev) {
    if (!areaArmed || !areaFrom) return;
    drawArea(areaFrom, pointOf(ev));
  }

  function onAreaUp(ev) {
    if (!areaArmed || !areaFrom) return;
    if (ev && ev.preventDefault) ev.preventDefault();
    if (ev && ev.stopPropagation) ev.stopPropagation();
    var to = pointOf(ev);
    var w = Math.abs(areaFrom.x - to.x);
    var h = Math.abs(areaFrom.y - to.y);
    var sx = globalThis.scrollX || 0;
    var sy = globalThis.scrollY || 0;
    if (w >= 4 && h >= 4) {
      areaRect = { x: Math.min(areaFrom.x, to.x) + sx, y: Math.min(areaFrom.y, to.y) + sy, w: w, h: h };
    }
    disarmArea();
  }

  function onAreaKey(ev) {
    if (areaArmed && ev && (ev.key === "Escape" || ev.keyCode === 27)) disarmArea();
  }

  function listenArea(on) {
    var doc = globalThis.document;
    if (!doc || !doc.addEventListener) return;
    var fn = on ? "addEventListener" : "removeEventListener";
    doc[fn]("mousedown", onAreaDown, true);
    doc[fn]("mousemove", onAreaMove, true);
    doc[fn]("mouseup", onAreaUp, true);
    doc[fn]("keydown", onAreaKey, true);
  }

  function armArea() {
    if (areaArmed) return "armed";
    disarm();
    areaArmed = true;
    areaFrom = null;
    areaRect = null;
    ensureAreaBox();
    listenArea(true);
    return "armed";
  }

  function disarmArea() {
    if (!areaArmed) return "idle";
    areaArmed = false;
    areaFrom = null;
    listenArea(false);
    if (areaBox && areaBox.parentNode) areaBox.parentNode.removeChild(areaBox);
    return "idle";
  }

  // "" while the user is still dragging, "idle" once it is over with nothing
  // to show, otherwise the rectangle — handed over once.
  function takeArea() {
    if (areaRect) {
      var out = areaRect;
      areaRect = null;
      return out;
    }
    return areaArmed ? "" : "idle";
  }

  tapConsole();

  globalThis.__orchPick = {
    arm: arm,
    disarm: disarm,
    take: take,
    isArmed: function () { return armed; },
    fmt: fmt,
    drainLog: drainLog,
    armArea: armArea,
    takeArea: takeArea,
    _internals: { selectorFor: selectorFor, markup: markup, appliedRules: appliedRules, payloadFor: payloadFor },
  };
})();
