// Apply the persisted theme before first paint, while CSP keeps all scripts
// external and same-origin.
(function () {
  "use strict";
  try {
    var pref = localStorage.getItem("marktoast-theme") || "auto";
    var dark = pref === "dark" || (pref === "auto" && matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.dataset.theme = dark ? "dark" : "light";
    document.documentElement.style.colorScheme = dark ? "dark" : "light";
  } catch (error) {}
})();
