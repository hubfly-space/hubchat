/*
 * Hubchat widget loader.
 *
 * This file runs on other people's websites. It is written by hand, ships no
 * framework, and does as close to nothing as possible until the visitor
 * actually asks for support.
 *
 * Contract:
 *   · never blocks rendering — no synchronous work beyond reading config
 *   · never touches the host page's globals beyond `window.Hubchat`
 *   · never fetches the interface until the launcher is shown or an API call
 *     needs it
 *   · queues every call made before boot completes, and replays them in order
 *
 * Everything visual lives in app.js, which is fetched lazily and mounted into
 * a shadow root.
 */
(function () {
  "use strict";

  var GLOBAL = "Hubchat";
  var host = document.currentScript
    ? new URL(document.currentScript.src).origin
    : window.location.origin;

  // The stub the install snippet defines may already hold queued calls.
  var existing = window[GLOBAL];
  var queue = (existing && existing.q) || [];

  var state = {
    booted: false,
    config: null,
    app: null,
    pending: queue.slice(),
  };

  function call(method, payload) {
    if (state.app) {
      state.app.handle(method, payload);
      return;
    }
    state.pending.push([method, payload]);
    // boot() stores the key before the public configuration arrives. Do not
    // start app.js against that half-configured object; the config callback
    // below drains pending commands once `remote` is available.
    if (method !== "boot" && state.config && state.config.remote) load();
  }

  var loading = false;

  function load() {
    if (loading || state.app) return;
    loading = true;

    // A module import rather than a script tag: it gives us the export map
    // directly and shares the browser's module cache if the host page happens
    // to load the same URL twice.
    import(host + "/widget/app.js")
      .then(function (module) {
        return module.mount({ host: host, config: state.config });
      })
      .then(function (app) {
        state.app = app;
        var pending = state.pending;
        state.pending = [];
        for (var i = 0; i < pending.length; i++) {
          if (pending[i][0] === "boot") continue;
          app.handle(pending[i][0], pending[i][1]);
        }
      })
      .catch(function (error) {
        loading = false;
        // Failing to load support must never break the host application.
        if (window.console && console.warn) {
          console.warn("[hubchat] interface failed to load:", error);
        }
      });
  }

  function boot(options) {
    if (state.booted) return;
    var nextConfig = options || {};

    if (!nextConfig.key) {
      if (window.console && console.warn) {
        console.warn("[hubchat] boot() requires a `key`.");
      }
      return;
    }

    state.booted = true;
    state.config = nextConfig;

    // Fetch the public configuration first. It is small, cacheable, and tells
    // us whether this origin is even allowed to render a launcher — so a
    // disallowed domain never downloads the interface at all.
    var url =
      host +
      "/api/v1/widget/config?key=" +
      encodeURIComponent(state.config.key) +
      "&url=" +
      encodeURIComponent(location.href) +
      "&language=" +
      encodeURIComponent((navigator.language || "").toLowerCase());

    fetch(url, { credentials: "omit" })
      .then(function (response) {
        if (!response.ok) throw new Error("widget configuration request failed");
        return response.json();
      })
      .then(function (config) {
        if (!config || !config.enabled) return;
        state.config.remote = config;

        // Honour the trigger without loading anything heavier than this file.
        var behavior = config.behavior || {};
        // A host can call show(), identify(), or startConversation() in the
        // same tick as boot(). Those calls are queued while config is being
        // fetched; once config arrives, a pending app command is itself a
        // request to lazy-load the interface even when the configured trigger
        // is manual. Without this drain, the first interaction is silently
        // stranded until the host calls the method a second time.
        var hasPendingCommand = state.pending.some(function (item) {
          return item[0] !== "boot" && item[0] !== "on";
        });
        switch (behavior.trigger) {
          case "immediate":
            load();
            break;
          case "delay":
            window.setTimeout(load, (behavior.delay_seconds || 0) * 1000);
            break;
          case "scroll":
            onScroll(behavior.scroll_percent || 50, load);
            break;
          case "manual":
          case "event":
            break;
          default:
            load();
        }
        if (hasPendingCommand) load();
      })
      .catch(function () {
        // Keep boot retryable after a transient network failure. Calls made
        // while the request was in flight remain queued and replay when a
        // later boot succeeds; a broken support endpoint must not strand the
        // host page in a permanently half-booted state.
        state.booted = false;
        state.config = null;
      });
  }

  function onScroll(percent, done) {
    function check() {
      var scrolled =
        (window.scrollY + window.innerHeight) / document.documentElement.scrollHeight;
      if (scrolled * 100 >= percent) {
        window.removeEventListener("scroll", check);
        done();
      }
    }
    window.addEventListener("scroll", check, { passive: true });
    check();
  }

  function hubchat(method, payload) {
    if (method === "boot") {
      boot(payload);
      return;
    }
    call(method, payload);
  }

  hubchat.q = [];
  window[GLOBAL] = hubchat;

  // Replay whatever the stub collected before this file arrived.
  for (var i = 0; i < queue.length; i++) {
    hubchat.apply(null, queue[i]);
  }
})();
