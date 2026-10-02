// Passkey enrolment, sign-in and step-up approvals (design D45). It only moves
// the browser's WebAuthn calls between the page and the server: every check is the
// server's. Without JavaScript the pages say so and the host's CLI answers instead.
(function () {
  "use strict";

  function toBuffer(s) {
    s = s.replace(/-/g, "+").replace(/_/g, "/");
    while (s.length % 4) {
      s += "=";
    }
    var bin = atob(s);
    var out = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) {
      out[i] = bin.charCodeAt(i);
    }
    return out.buffer;
  }

  function toBase64url(buf) {
    var bytes = new Uint8Array(buf);
    var s = "";
    for (var i = 0; i < bytes.length; i++) {
      s += String.fromCharCode(bytes[i]);
    }
    return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  function csrf() {
    var m = document.querySelector('meta[name="csrf-token"]');
    return m ? m.content : "";
  }

  function say(button, text) {
    var box = button.closest("article, main");
    var el = box && box.querySelector("[data-passkey-status]");
    if (el) {
      el.textContent = text;
      el.hidden = false;
    }
  }

  function post(url, body, headers) {
    var h = { "Content-Type": "application/json", "X-CSRF-Token": csrf() };
    Object.keys(headers || {}).forEach(function (k) {
      h[k] = headers[k];
    });
    return fetch(url, { method: "POST", credentials: "same-origin", headers: h, body: JSON.stringify(body) }).then(function (r) {
      return r.json().then(function (j) {
        if (!r.ok) {
          throw new Error(j.error || "The request was refused.");
        }
        return j;
      });
    });
  }

  function created(c) {
    return {
      id: c.id,
      rawId: toBase64url(c.rawId),
      type: c.type,
      response: {
        clientDataJSON: toBase64url(c.response.clientDataJSON),
        attestationObject: toBase64url(c.response.attestationObject),
        transports: c.response.getTransports ? c.response.getTransports() : [],
      },
    };
  }

  function asserted(c) {
    return {
      id: c.id,
      rawId: toBase64url(c.rawId),
      type: c.type,
      response: {
        clientDataJSON: toBase64url(c.response.clientDataJSON),
        authenticatorData: toBase64url(c.response.authenticatorData),
        signature: toBase64url(c.response.signature),
        userHandle: c.response.userHandle ? toBase64url(c.response.userHandle) : "",
      },
    };
  }

  function get(options) {
    var p = options.publicKey;
    p.challenge = toBuffer(p.challenge);
    (p.allowCredentials || []).forEach(function (c) {
      c.id = toBuffer(c.id);
    });
    return navigator.credentials.get({ publicKey: p });
  }

  function enrol(button) {
    return post("/passkey/enrol/begin", { token: button.dataset.token }).then(function (r) {
      var p = r.options.publicKey;
      p.challenge = toBuffer(p.challenge);
      p.user.id = toBuffer(p.user.id);
      (p.excludeCredentials || []).forEach(function (c) {
        c.id = toBuffer(c.id);
      });
      return navigator.credentials.create({ publicKey: p }).then(function (c) {
        return post("/passkey/enrol/finish", created(c), { "X-Ceremony": r.ceremony });
      });
    }).then(function () {
      say(button, "The passkey is enrolled. You can sign in with it now.");
    });
  }

  function login(button) {
    return post("/passkey/login/begin", {}).then(function (r) {
      return get(r.options).then(function (c) {
        return post("/passkey/login/finish", asserted(c), { "X-Ceremony": r.ceremony });
      });
    }).then(function (r) {
      window.location.assign(r.next || "/");
    });
  }

  function stepUp(button) {
    var id = encodeURIComponent(button.dataset.decision);
    var reasonBox = document.getElementById(button.dataset.reason);
    var q = "?option=" + encodeURIComponent(button.dataset.stepup) + "&reason=" + encodeURIComponent(reasonBox ? reasonBox.value : "");
    return post("/decisions/" + id + "/stepup/begin", {}).then(function (r) {
      return get(r.options).then(function (c) {
        return post("/decisions/" + id + "/stepup/finish" + q, asserted(c), { "X-Ceremony": r.ceremony });
      });
    }).then(function (r) {
      window.location.assign(r.location);
    });
  }

  document.addEventListener("click", function (e) {
    var button = e.target.closest("[data-passkey], [data-stepup]");
    if (!button) {
      return;
    }
    e.preventDefault();
    if (!window.PublicKeyCredential) {
      say(button, "This browser has no passkey support.");
      return;
    }
    var run = button.dataset.passkey === "enrol" ? enrol : button.dataset.passkey === "login" ? login : stepUp;
    button.disabled = true;
    run(button).catch(function (err) {
      say(button, err && err.message ? err.message : "The passkey was not used.");
    }).then(function () {
      button.disabled = false;
    });
  });
})();
