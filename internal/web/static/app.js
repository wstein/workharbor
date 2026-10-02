// The live transcript resumes where it left off. The browser's own EventSource
// does that when it reconnects, but htmx's SSE extension makes a new one when
// the browser gives up, which would replay the page's first events again. This
// remembers the last event ID of each stream and asks for the ones after it.
// It must run before sse.js, which only sets its own function if none exists.
(function () {
  "use strict";
  if (!window.htmx) {
    return;
  }
  var last = {};
  window.htmx.createEventSource = function (url) {
    var u = new URL(url, window.location.href);
    var key = u.pathname;
    if (last[key]) {
      u.searchParams.set("since", last[key]);
    }
    var source = new EventSource(u.toString(), { withCredentials: true });
    source.addEventListener("ev", function (e) {
      if (e.lastEventId) {
        last[key] = e.lastEventId;
      }
    });
    return source;
  };
})();
