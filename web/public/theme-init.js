// Applies the saved admin theme before first paint, so dark-mode users never
// see a white flash. External file because the CSP forbids inline scripts.
(function () {
  try {
    var t = localStorage.getItem('neo-theme') || 'system';
    if (t === 'system') t = matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', t);
  } catch (e) {}
})();
