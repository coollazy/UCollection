// Converts every server-rendered <time datetime="..."> (UTC ISO8601)
// into the browser's local time — the server has no way to know the
// visitor's timezone from an HTTP request, so this has to happen
// client-side.
document.querySelectorAll('time[datetime]').forEach((el) => {
  const d = new Date(el.getAttribute('datetime'));
  if (!Number.isNaN(d.getTime())) {
    el.textContent = d.toLocaleString();
  }
});
