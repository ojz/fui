/* fui sharedLocalStorage.js
 *
 * Contract: include <script src="/sharedLocalStorage.js"></script> in a page
 * served at /p/<id>. window.localStorage is then replaced by a server-backed
 * store scoped to this page: setItem/getItem/removeItem/clear work as usual,
 * but every write is persisted in the server's SQLite database (surviving
 * reboots) and readable by the agent at GET /api/state/<id> and
 * GET /api/wait/<id>.
 *
 * The page also holds an EventSource on /events/<id>: the server pushes the
 * full state map whenever it changes elsewhere (the agent posting a key,
 * another browser tab, another page shown under the same id) and this script
 * fires the standard "storage" event for every changed key — the same event
 * window already fires when another tab writes to real localStorage. No new
 * API: listen for it as usual. When the agent republishes the page's HTML the
 * server sends a "reload" event and this script reloads the tab.
 *
 * window.done() signals completion and unblocks the agent's /api/wait call.
 */
(function () {
  "use strict";

  var m = location.pathname.match(/^\/p\/([^\/]+)/);
  var pageId = m ? decodeURIComponent(m[1]) : "default";
  var stateURL = "/api/state/" + encodeURIComponent(pageId);
  var cache = {};

  // Initial load is a synchronous XHR on purpose: localStorage is a sync API
  // and page scripts expect their data to be there on first read. This runs
  // once against localhost, so the stall is negligible.
  try {
    var xhr = new XMLHttpRequest();
    xhr.open("GET", stateURL, false);
    xhr.send();
    if (xhr.status === 200) cache = JSON.parse(xhr.responseText) || {};
  } catch (e) {
    console.warn("sharedLocalStorage: could not load initial state", e);
  }

  function push(key, value) {
    fetch(stateURL, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Fui": "1" },
      body: JSON.stringify({ key: key, value: value }),
      keepalive: true
    }).catch(function (e) {
      console.warn("sharedLocalStorage: sync failed for", key, e);
    });
  }

  var storage = {
    getItem: function (k) {
      k = String(k);
      return Object.prototype.hasOwnProperty.call(cache, k) ? cache[k] : null;
    },
    setItem: function (k, v) {
      k = String(k); v = String(v);
      cache[k] = v;
      push(k, v);
    },
    removeItem: function (k) {
      k = String(k);
      delete cache[k];
      push(k, null);
    },
    clear: function () {
      cache = {};
      fetch(stateURL, { method: "DELETE", headers: { "X-Fui": "1" }, keepalive: true });
    },
    key: function (i) {
      var keys = Object.keys(cache);
      return i >= 0 && i < keys.length ? keys[i] : null;
    },
    get length() {
      return Object.keys(cache).length;
    }
  };

  try {
    Object.defineProperty(window, "localStorage", {
      get: function () { return storage; },
      configurable: true
    });
  } catch (e) {
    // Some browsers refuse to shadow localStorage; expose a fallback.
    window.sharedLocalStorage = storage;
  }

  // Apply a full state map pushed by the server: update the cache and fire
  // the native "storage" event for every key that was added, changed or
  // removed. storageArea must be null — a non-Storage object throws.
  function apply(fresh) {
    var keys = {};
    for (var k in cache) keys[k] = true;
    for (var k2 in fresh) keys[k2] = true;
    for (var key in keys) {
      var oldValue = Object.prototype.hasOwnProperty.call(cache, key) ? cache[key] : null;
      var newValue = Object.prototype.hasOwnProperty.call(fresh, key) ? fresh[key] : null;
      if (oldValue === newValue) continue;
      if (newValue === null) delete cache[key]; else cache[key] = newValue;
      window.dispatchEvent(new StorageEvent("storage", {
        key: key, oldValue: oldValue, newValue: newValue, storageArea: null
      }));
    }
  }

  // Server-pushed updates over Server-Sent Events: no polling, no idle
  // traffic. Pages can stay open for hours; EventSource reconnects on its own
  // after a drop (server restart, sleep/resume), so there is nothing to retry
  // here by hand.
  var es = new EventSource("/events/" + encodeURIComponent(pageId));
  es.onmessage = function (ev) {
    try {
      var fresh = JSON.parse(ev.data);
      if (fresh && typeof fresh === "object") apply(fresh);
    } catch (e) {
      console.warn("sharedLocalStorage: bad state push", e);
    }
  };
  // The agent republished this page's HTML: show the new version.
  es.addEventListener("reload", function () { location.reload(); });

  window.done = function () {
    return fetch("/api/done/" + encodeURIComponent(pageId), {
      method: "POST",
      headers: { "X-Fui": "1" },
      keepalive: true
    });
  };
})();
