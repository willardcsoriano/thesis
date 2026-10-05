// Applies the reader's saved light/dark choice before the first paint, so a
// reader who chose dark never sees a flash of light. Loaded as a plain,
// blocking script in <head>; the toggle itself lives in app.js. With no
// saved choice the page follows the system setting through CSS.
(function () {
  try {
    var theme = localStorage.getItem("synapsebot.theme");
    if (theme === "light" || theme === "dark") {
      document.documentElement.dataset.theme = theme;
      document.documentElement.style.colorScheme = theme;
    }
  } catch (e) {
    // Storage blocked (private mode, settings): follow the system setting.
  }
})();
