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

// Keyboard shortcuts for a tablet with a keyboard (design §9.6). They only move the
// focus and follow the page's own links; nothing here answers a Decision, and a
// key typed into a field is never a shortcut.
(function () {
  "use strict";
  var pending = 0;

  function typing(el) {
    return !!el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));
  }

  function step(delta) {
    var links = Array.prototype.slice.call(document.querySelectorAll(".tasklist a"));
    if (!links.length) {
      return;
    }
    var at = links.indexOf(document.activeElement);
    if (at < 0) {
      at = links.findIndex(function (a) {
        return a.getAttribute("aria-current") === "page";
      });
    }
    var next = links[Math.min(Math.max(at + delta, 0), links.length - 1)];
    if (next) {
      next.focus();
    }
  }

  document.addEventListener("keydown", function (e) {
    var el = e.target;
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && el && el.tagName === "TEXTAREA" && el.form) {
      e.preventDefault();
      if (el.form.requestSubmit) {
        el.form.requestSubmit();
      }
      return;
    }
    if (e.ctrlKey || e.metaKey || e.altKey || typing(el)) {
      return;
    }
    if (pending && Date.now() - pending < 1500) {
      pending = 0;
      var link = document.querySelector('a[data-shortcut="' + e.key + '"]');
      if (link) {
        e.preventDefault();
        link.click();
      }
      return;
    }
    switch (e.key) {
      case "g":
        pending = Date.now();
        break;
      case "j":
        step(1);
        break;
      case "k":
        step(-1);
        break;
      case "m":
      case "/":
        var box = document.getElementById("message");
        if (box) {
          e.preventDefault();
          box.focus();
        }
        break;
      case "?":
        var help = document.getElementById("shortcuts");
        if (help) {
          help.open = !help.open;
        }
        break;
      case "Escape":
        if (document.activeElement && document.activeElement.blur) {
          document.activeElement.blur();
        }
        break;
    }
  });
})();

// Copy diagnostic commands as text; host commands still run in Terminal.
document.addEventListener("click", function (event) {
  var button = event.target.closest("[data-copy-command]");
  if (!button) {
    return;
  }
  var block = button.closest(".setup-copy");
  var status = block.querySelector('[role="status"]');
  if (!navigator.clipboard || !navigator.clipboard.writeText) {
    status.textContent = "Select and copy the command above.";
    return;
  }
  navigator.clipboard.writeText(block.querySelector("code").textContent).then(function () {
    status.textContent = "Copied.";
  }, function () {
    status.textContent = "Could not copy. Select and copy the command above.";
  });
});
