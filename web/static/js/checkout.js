// Checkout page live status: polls the server every 5s for the order's
// status fragment and swaps it in, and runs a smooth 1s local countdown for
// PENDING orders. Hand-written vanilla JS with zero dependencies, matching
// this project's localtime.js/admin.js convention (the tech design's mention
// of htmx was never realized — the project vendors no htmx). Loaded at the
// end of <body>, so the DOM is ready when this runs.
//
// The server is the source of truth: every response (initial page render and
// every poll) carries a fresh #status-region whose data-* attributes drive
// this script — data-remaining (relative seconds, so no client clock drift),
// data-countdown (show the countdown?), data-terminal (stop polling?),
// data-status-url (where to poll).
(function () {
  var remaining = 0;
  var countdownTimer = null;
  var pollTimer = null;

  function region() {
    return document.getElementById('status-region');
  }

  // Format whole seconds as H:MM:SS (or M:SS under an hour).
  function fmt(total) {
    if (total < 0) total = 0;
    var h = Math.floor(total / 3600);
    var m = Math.floor((total % 3600) / 60);
    var s = total % 60;
    var mm = String(m).padStart(2, '0');
    var ss = String(s).padStart(2, '0');
    return h > 0 ? h + ':' + mm + ':' + ss : m + ':' + ss;
  }

  function paintCountdown() {
    var el = region();
    var span = el && el.querySelector('[data-countdown-display]');
    if (span) span.textContent = fmt(remaining);
  }

  function stopCountdown() {
    if (countdownTimer !== null) {
      clearInterval(countdownTimer);
      countdownTimer = null;
    }
  }

  function stopPolling() {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  function tickCountdown() {
    if (remaining > 0) remaining -= 1;
    paintCountdown();
  }

  // (Re)configure the countdown from the current #status-region data-*, and
  // stop polling once the server reports a terminal state.
  function sync() {
    var el = region();
    if (!el) {
      stopCountdown();
      stopPolling();
      return;
    }
    var terminal = el.dataset.terminal === 'true';
    var showCountdown = el.dataset.countdown === 'true';
    remaining = parseInt(el.dataset.remaining, 10);
    if (isNaN(remaining)) remaining = 0;

    if (showCountdown && !terminal) {
      paintCountdown();
      if (countdownTimer === null) countdownTimer = setInterval(tickCountdown, 1000);
    } else {
      stopCountdown();
    }

    if (terminal) stopPolling();
  }

  function poll() {
    var el = region();
    if (!el) {
      stopPolling();
      return;
    }
    var url = el.dataset.statusUrl;
    if (!url) return;
    fetch(url, { headers: { Accept: 'text/html' } })
      .then(function (res) {
        if (!res.ok) return null; // transient (e.g. 5xx) — keep polling
        return res.text();
      })
      .then(function (html) {
        if (html === null) return;
        var current = region();
        if (current) current.outerHTML = html; // fresh #status-region + data-*
        sync(); // realign countdown / stop polling if now terminal
      })
      .catch(function () {
        // network hiccup — keep polling on the next tick
      });
  }

  function start() {
    sync();
    var el = region();
    if (el && el.dataset.terminal !== 'true') {
      pollTimer = setInterval(poll, 5000);
    }
  }

  start();
})();
