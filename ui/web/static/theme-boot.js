// Stamps the saved theme and scale on <html> before the first paint.
//
// This is a file rather than an inline <script> in the page head because both
// pages are served under a Content-Security-Policy with script-src 'self': an
// inline block would be dropped, and the page would flash the wrong theme on
// every load — the exact thing this code exists to prevent. A classic <script>
// in <head> blocks rendering the same way inline does, so nothing flashes.
//
// Web-only: the VS Code webview follows the editor's theme instead.
(function () {
  try {
    var saved = localStorage.getItem("orchestra.theme");
    if (saved === "light" || saved === "dark") {
      document.documentElement.setAttribute("data-theme", saved);
    }
    // Same reason, and more visible: the scale changes every measurement on
    // the page, so applying it after the first paint reflows the lot.
    var scale = localStorage.getItem("orchestra.scale");
    if (/^(100|110|125|150|175|200)$/.test(scale || "")) {
      document.documentElement.setAttribute("data-scale", scale);
    }
  } catch (e) {
    // Storage can throw outright in a locked-down browser.
  }
})();
