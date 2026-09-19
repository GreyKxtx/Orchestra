// The developer tools, painted in this app's colours.
//
// Runs inside the tools' own page: once before it boots, as an initialization
// script, and again whenever the app repaints. The tools define every colour
// they use as a custom property on the root — Chromium's --sys-color-* and,
// in Edge, a Fluent layer over them — so this reads those properties and
// restates each one in the palette the app sent. A neutral becomes the app's
// neutral of the same depth; the browser's blue becomes the app's accent.
// Colours that carry meaning — errors, diffs, syntax — are left as they are.
//
// Every declaration is written with !important: the tools' own stylesheets
// are adopted ones, and those cascade after anything a page adds. Colour
// only: the type, the icons and the arrangement of the panels are Chromium's.
(function () {
  var STYLE_ID = "orchestra-palette";
  var KEY = "orchestra-palette";
  // `blue` stays too: the syntax colours are spelled in terms of it, and a
  // tag, an attribute and a link all in the accent would be one colour.
  var KEEP = /token|syntax|error|warning|red|green|yellow|orange|pink|purple|cyan|blue|tertiary|gradient|deleted|inserted|scrim|shadow|highlight/i;
  var NEUTRALS = ["bg", "surface", "surface2", "border", "muted2", "muted", "fgDim", "fg"];

  var timers = [];
  var watching = false;

  /* ---- colours ---------------------------------------------------------- */

  /** `#rgb`, `#rrggbb`, `#rrggbbaa`, `rgb(r g b / a)`, `rgba(r, g, b, a)` — or null. */
  function parse(value) {
    var s = String(value || "").trim();
    var m = /^#([0-9a-f]{3,8})$/i.exec(s);
    if (m) {
      var h = m[1];
      if (h.length === 3 || h.length === 4) h = h.replace(/./g, function (c) { return c + c; });
      if (h.length !== 6 && h.length !== 8) return null;
      return {
        r: parseInt(h.slice(0, 2), 16),
        g: parseInt(h.slice(2, 4), 16),
        b: parseInt(h.slice(4, 6), 16),
        a: h.length === 8 ? parseInt(h.slice(6, 8), 16) / 255 : 1,
      };
    }
    m = /^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)(?:\s*[\/,]\s*([\d.]+)(%?))?\s*\)$/i.exec(s);
    if (!m) return null;
    var a = 1;
    if (m[4] !== undefined) a = m[5] ? Number(m[4]) / 100 : Number(m[4]);
    return { r: Number(m[1]), g: Number(m[2]), b: Number(m[3]), a: a };
  }

  /** Lightness, saturation and hue, each 0..1 (hue in degrees). */
  function hsl(c) {
    var r = c.r / 255, g = c.g / 255, b = c.b / 255;
    var max = Math.max(r, g, b), min = Math.min(r, g, b);
    var l = (max + min) / 2;
    var d = max - min;
    var s = d === 0 ? 0 : d / (1 - Math.abs(2 * l - 1));
    var h = 0;
    if (d !== 0) {
      if (max === r) h = ((g - b) / d) % 6;
      else if (max === g) h = (b - r) / d + 2;
      else h = (r - g) / d + 4;
      h = (h * 60 + 360) % 360;
    }
    return { l: l, s: s, h: h };
  }

  // Chroma, not HSL saturation: a near-white like rgb(253 252 251) is a grey
  // to the eye, and saturation blows up next to white and black.
  function chroma(c) { return (Math.max(c.r, c.g, c.b) - Math.min(c.r, c.g, c.b)) / 255; }
  function isNeutral(c) { return chroma(c) < 0.08; }
  function isBlue(c) { var h = hsl(c).h; return chroma(c) >= 0.08 && h >= 190 && h <= 260; }

  function fmt(c, a) {
    var alpha = a === undefined ? c.a : a;
    return "rgb(" + Math.round(c.r) + " " + Math.round(c.g) + " " + Math.round(c.b) +
      (alpha >= 1 ? ")" : " / " + Math.round(alpha * 1000) / 1000 + ")");
  }

  /* ---- the mapping ------------------------------------------------------ */

  /**
   * What each of the tools' colours becomes in `palette`, given `originals`.
   * Pure: the same inputs give the same answer, which is what makes repainting
   * safe to do as often as the page changes.
   *
   * @param {Record<string, {r:number,g:number,b:number,a:number}>} originals
   * @param {Record<string, string>} palette
   * @returns {Array<[string, string]>}
   */
  function mapping(originals, palette) {
    var ours = [];
    for (var i = 0; i < NEUTRALS.length; i++) {
      var c = parse(palette[NEUTRALS[i]]);
      if (c) ours.push({ c: c, l: hsl(c).l });
    }
    var accent = parse(palette.accent);
    var bg = parse(palette.bg);
    if (ours.length < 2 || !accent || !bg) return [];

    // A grey is placed by where it sits between the tools' own ground and
    // their own text, and lands where that is between our ground and our
    // text — so their panels become our ground whatever they are in
    // absolute terms, and what lies beyond either end is clamped to it.
    // Without those two to anchor on, their darkest and lightest do.
    var names = Object.keys(originals);
    var theirBg = originals["--sys-color-cdt-base-container"] || originals["--sys-color-surface"];
    var theirFg = originals["--sys-color-on-surface"];
    var lo = 1, hi = 0;
    for (var j = 0; j < names.length; j++) {
      var o = originals[names[j]];
      if (o.a < 0.99 || !isNeutral(o)) continue;
      var l = hsl(o).l;
      if (l < lo) lo = l;
      if (l > hi) hi = l;
    }
    var fromL = theirBg && isNeutral(theirBg) ? hsl(theirBg).l : lo;
    var toL = theirFg && isNeutral(theirFg) ? hsl(theirFg).l : hi;
    if (fromL === toL) return [];
    var fg = parse(palette.fg) || ours[ours.length - 1].c;
    var bgL = hsl(bg).l;
    var fgL = hsl(fg).l;

    var out = [];
    for (var n = 0; n < names.length; n++) {
      var name = names[n];
      if (KEEP.test(name)) continue;
      var orig = originals[name];
      if (isNeutral(orig)) {
        var t = (hsl(orig).l - fromL) / (toL - fromL);
        if (t < 0) t = 0;
        if (t > 1) t = 1;
        var target = bgL + t * (fgL - bgL);
        var best = ours[0];
        for (var m = 1; m < ours.length; m++) {
          if (Math.abs(ours[m].l - target) < Math.abs(best.l - target)) best = ours[m];
        }
        out.push([name, fmt(best.c, orig.a)]);
      } else if (isBlue(orig)) {
        // A blue that stands off the ground is text or a line: the accent
        // itself. One that sits near the ground is a container — a
        // selection, a focused row — and gets the accent as a tint.
        if (Math.abs(hsl(orig).l - bgL) > 0.3) {
          out.push([name, fmt(accent, orig.a)]);
        } else {
          out.push([name, fmt({
            r: accent.r * 0.35 + bg.r * 0.65,
            g: accent.g * 0.35 + bg.g * 0.65,
            b: accent.b * 0.35 + bg.b * 0.65,
            a: 1,
          }, orig.a)]);
        }
      }
    }
    return out;
  }

  /* ---- the page --------------------------------------------------------- */

  /** Every custom property any stylesheet of this document declares. */
  function declared() {
    var names = {};
    function walk(rules) {
      for (var i = 0; i < (rules ? rules.length : 0); i++) {
        var rule = rules[i];
        if (rule.style) {
          for (var j = 0; j < rule.style.length; j++) {
            var p = rule.style[j];
            if (p.indexOf("--") === 0) names[p] = true;
          }
        }
        if (rule.cssRules) walk(rule.cssRules);
      }
    }
    var sheets = [];
    var i;
    for (i = 0; i < document.styleSheets.length; i++) sheets.push(document.styleSheets[i]);
    var adopted = document.adoptedStyleSheets || [];
    for (i = 0; i < adopted.length; i++) sheets.push(adopted[i]);
    for (i = 0; i < sheets.length; i++) {
      try { walk(sheets[i].cssRules); } catch (e) { /* a sheet from elsewhere */ }
    }
    return Object.keys(names);
  }

  /**
   * What every property is in the tools' own hand, read fresh each time with
   * our stylesheet switched off for the reading. Fresh, because the tools
   * boot in their light theme and turn dark only once they have read their
   * setting: a value remembered from before that is the wrong theme's.
   */
  function collect() {
    var originals = {};
    var root = document.documentElement;
    if (!root) return originals;
    var ours = document.getElementById(STYLE_ID);
    if (ours) ours.disabled = true;
    var computed = getComputedStyle(root);
    var names = declared();
    for (var i = 0; i < names.length; i++) {
      var c = parse(computed.getPropertyValue(names[i]));
      if (c) originals[names[i]] = c;
    }
    if (ours) ours.disabled = false;
    return originals;
  }

  function apply(palette) {
    if (!document.head) return false;
    var pairs = mapping(collect(), palette);
    var css = "";
    for (var i = 0; i < pairs.length; i++) css += pairs[i][0] + ":" + pairs[i][1] + " !important;";
    var el = document.getElementById(STYLE_ID);
    if (!el) {
      el = document.createElement("style");
      el.id = STYLE_ID;
      document.head.appendChild(el);
    }
    el.textContent = css ? ":root{" + css + "}" : "";
    return true;
  }

  /**
   * Paint the tools in `json`, a palette the app measured off its own page.
   * The tools bring their stylesheets in as they load their panels, so the
   * paint is done now and again a few times after, catching what arrived in
   * between. The palette is kept, so the page that comes back from a reload
   * comes back wearing it.
   *
   * @param {string} json
   */
  function paint(json) {
    var palette;
    try { palette = JSON.parse(json); } catch (e) { return; }
    if (!palette || typeof palette !== "object") return;
    try { localStorage.setItem(KEY, json); } catch (e) { /* no storage; the paint still holds */ }
    for (var i = 0; i < timers.length; i++) clearTimeout(timers[i]);
    timers = [];
    var go = function () { apply(palette); };
    if (!go()) document.addEventListener("DOMContentLoaded", go, { once: true });
    var delays = [500, 1500, 4000, 10000];
    for (var j = 0; j < delays.length; j++) timers.push(setTimeout(go, delays[j]));
    // The tools turn dark by putting a class on the root; that is a new set
    // of colours to read.
    if (!watching && typeof MutationObserver === "function" && document.documentElement) {
      watching = true;
      new MutationObserver(function () { setTimeout(go, 50); })
        .observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    }
  }

  paint.parse = parse;
  paint.mapping = mapping;
  globalThis.__orchestraPaint = paint;
})();
