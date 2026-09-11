// Highlights the sidebar nav link matching the current page and keeps its
// <details> settings group open if the active link is inside one. Pure
// enhancement — the sidebar is fully navigable without this running, it
// only adds the ".active" visual state (no per-page "active" field is
// threaded through every handler's template data for this).
document.querySelectorAll('.sidebar .nav a[href]').forEach((a) => {
  const here = window.location.pathname;
  const linkPath = new URL(a.getAttribute('href'), window.location.origin).pathname;
  if (here === linkPath || here.startsWith(linkPath + '/')) {
    a.classList.add('active');
    const details = a.closest('details');
    if (details) details.open = true;
  }
});
