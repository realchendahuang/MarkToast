// Apply persisted theme and language before first paint while keeping all
// scripts external and same-origin under the hub's strict CSP.
(function () {
  "use strict";
  try {
    var pref = localStorage.getItem("marktoast-theme") || "auto";
    var dark = pref === "dark" || (pref === "auto" && matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.dataset.theme = dark ? "dark" : "light";
    document.documentElement.style.colorScheme = dark ? "dark" : "light";
    var meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.setAttribute("content", dark ? "#141518" : "#f5f5f7");
    var lang = localStorage.getItem("marktoast-lang");
    if (lang === "en") document.documentElement.lang = "en";
  } catch (error) {}
})();
