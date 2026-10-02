// Registers the service worker of the installed app (issue #33). It needs HTTPS or
// localhost, and does nothing where the browser has no service workers.
(function () {
  "use strict";
  if (!("serviceWorker" in navigator)) {
    return;
  }
  window.addEventListener("load", function () {
    navigator.serviceWorker.register("/sw.js", { scope: "/" }).catch(function () {
      // The app works without it: only the offline page and the cache are lost.
    });
  });
})();
