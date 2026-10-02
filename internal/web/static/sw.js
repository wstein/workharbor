// The service worker of the installed app (design §9.3, issue #33). It keeps the
// app shell and nothing else: the styles, scripts, icons and the offline page.
// A page, a transcript, a diff, a Decision or any answer is never put in a cache;
// those requests are not handled here at all, and go straight to the network. The
// cache holds only the files named below.
"use strict";

const CACHE = "whr-shell-v1";
const SHELL = [
  "/static/app.css",
  "/static/app.js",
  "/static/htmx.min.js",
  "/static/sse.js",
  "/static/passkey.js",
  "/static/pwa.js",
  "/static/icons/favicon.svg",
  "/static/icons/apple-touch-icon.png",
  "/static/icons/android-chrome-192x192.png",
  "/static/icons/android-chrome-512x512.png",
  "/offline",
];

self.addEventListener("install", function (event) {
  event.waitUntil(
    caches
      .open(CACHE)
      .then(function (cache) {
        return cache.addAll(SHELL);
      })
      .then(function () {
        return self.skipWaiting();
      })
  );
});

self.addEventListener("activate", function (event) {
  event.waitUntil(
    caches
      .keys()
      .then(function (keys) {
        return Promise.all(
          keys
            .filter(function (k) {
              return k !== CACHE;
            })
            .map(function (k) {
              return caches.delete(k);
            })
        );
      })
      .then(function () {
        return self.clients.claim();
      })
  );
});

self.addEventListener("fetch", function (event) {
  const req = event.request;
  if (req.method !== "GET") {
    return;
  }
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) {
    return;
  }
  // A page: always the network. When it is not reachable, say so, and show nothing
  // from memory.
  if (req.mode === "navigate") {
    event.respondWith(
      fetch(req).catch(function () {
        return caches.match("/offline");
      })
    );
    return;
  }
  // The shell files only, from the cache first, refreshed when it is online. Any
  // other path (events, answers, JSON) is not handled and goes to the network.
  if (SHELL.indexOf(url.pathname) === -1) {
    return;
  }
  event.respondWith(
    caches.match(url.pathname).then(function (hit) {
      const fresh = fetch(req)
        .then(function (res) {
          if (res.ok) {
            const copy = res.clone();
            caches.open(CACHE).then(function (c) {
              return c.put(url.pathname, copy);
            });
          }
          return res;
        })
        .catch(function () {
          return hit;
        });
      return hit || fresh;
    })
  );
});
