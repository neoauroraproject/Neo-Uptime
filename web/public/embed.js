/*!
 * Neo Uptime status widget — https://github.com/neoauroraproject/Neo-Uptime
 *
 * <div id="neo-status"></div>
 * <script src="https://uptime.example.com/embed.js" data-target="#neo-status"
 *   data-server="https://uptime.example.com" data-slug="main" data-key="sk_…"
 *   data-lang="fa" data-theme="auto" async></script>
 *
 * Options (data-*): server, slug, key (required); target (CSS selector, default:
 * right after the script); lang (fa | en, default: the page's language);
 * theme (auto | light | dark); refresh (seconds, default 60, minimum 15);
 * title (false hides the heading); history (false hides the bars).
 */
(function () {
  'use strict'
  var script = document.currentScript || document.querySelector('script[src*="embed.js"][data-slug]')
  if (!script) return
  var ds = script.dataset
  var server = (ds.server || new URL(script.src, location.href).origin).replace(/\/+$/, '')
  var slug = ds.slug
  var key = ds.key
  var refresh = Math.max(15, Number(ds.refresh) || 60) * 1000
  var themeOpt = ds.theme || 'auto'
  var showTitle = ds.title !== 'false'
  var showHistory = ds.history !== 'false'

  var host = document.createElement('div')
  host.className = 'neo-uptime-widget'
  var target = ds.target && document.querySelector(ds.target)
  if (target) target.appendChild(host)
  else script.parentNode.insertBefore(host, script.nextSibling)
  var root = host.attachShadow ? host.attachShadow({ mode: 'open' }) : host

  var T = {
    en: {
      operational: 'All systems operational', partial_outage: 'Partial outage', major_outage: 'Major outage',
      maintenance: 'Under maintenance', unknown: 'Waiting for data',
      up: 'Operational', down: 'Down', pending: 'Degraded', paused: 'Paused', maint: 'Maintenance', none: 'No data',
      updated: 'Updated', uptime: 'uptime', error: 'Status is unavailable right now.', loading: 'Loading status…',
      ms: 'ms',
    },
    fa: {
      operational: 'همهٔ سرویس‌ها فعال‌اند', partial_outage: 'اختلال جزئی', major_outage: 'اختلال گسترده',
      maintenance: 'در حال نگهداری', unknown: 'در انتظار داده',
      up: 'فعال', down: 'قطع', pending: 'کاهش کارایی', paused: 'متوقف', maint: 'نگهداری', none: 'بدون داده',
      updated: 'به‌روزرسانی', uptime: 'آپتایم', error: 'وضعیت در حال حاضر در دسترس نیست.', loading: 'در حال بارگذاری وضعیت…',
      ms: 'میلی‌ثانیه',
    },
  }
  function pickLang(pageLang) {
    var l = (ds.lang || '').toLowerCase()
    if (l === 'fa' || l === 'en') return l
    if (pageLang === 'fa' || pageLang === 'en') return pageLang
    var doc = (document.documentElement.lang || navigator.language || 'en').toLowerCase()
    return doc.indexOf('fa') === 0 ? 'fa' : 'en'
  }
  var lang = pickLang('')

  function num(s) {
    s = String(s)
    if (lang !== 'fa') return s
    return s.replace(/[0-9]/g, function (d) { return '۰۱۲۳۴۵۶۷۸۹'[d] }).replace(/\./g, '٫').replace(/%/g, '٪')
  }
  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
    })
  }
  function pct(r) {
    if (r == null) return '–'
    var v = r * 100
    return num((v >= 99.995 ? '100' : v.toFixed(v >= 99 ? 2 : 1)) + '%')
  }
  var colors = { up: 'var(--up)', down: 'var(--down)', pending: 'var(--pend)', maintenance: 'var(--maint)', paused: 'var(--mute)', unknown: 'var(--mute)' }
  var word = { up: 'up', down: 'down', pending: 'pending', maintenance: 'maint', paused: 'paused', unknown: 'none' }
  var bar = { u: 'var(--up)', d: 'var(--down)', p: 'var(--pend)', m: 'var(--maint)' }
  var overallColor = { operational: 'var(--up)', partial_outage: 'var(--pend)', major_outage: 'var(--down)', maintenance: 'var(--maint)', unknown: 'var(--mute)' }

  var css =
    ':host{all:initial;display:block}' +
    '.w{container-type:inline-size;--bg:#ffffff;--fg:#0f1411;--sub:#6b716d;--line:#e8eae7;--mute:#b9bdb9;--empty:#eef0ed;--up:#16a34a;--down:#e5484d;--pend:#f59e0b;--maint:#6366f1;' +
    'font:14px/1.45 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,"Vazirmatn",Tahoma,sans-serif;color:var(--fg);background:var(--bg);' +
    'border:1px solid var(--line);border-radius:16px;padding:18px 18px 14px;box-sizing:border-box;max-width:100%}' +
    '.w.dark{--bg:#0d110e;--fg:#eef2ef;--sub:#8a938d;--line:#1f2621;--mute:#4a524c;--empty:#1a201b;--up:#22c55e;--down:#f2555a;--pend:#fbbf24;--maint:#818cf8}' +
    '@media (prefers-color-scheme:dark){.w.auto{--bg:#0d110e;--fg:#eef2ef;--sub:#8a938d;--line:#1f2621;--mute:#4a524c;--empty:#1a201b;--up:#22c55e;--down:#f2555a;--pend:#fbbf24;--maint:#818cf8}}' +
    '*{box-sizing:border-box}' +
    '.top{display:flex;align-items:center;gap:10px;margin-bottom:12px}' +
    '.dot{width:10px;height:10px;border-radius:50%;flex:none;box-shadow:0 0 0 4px color-mix(in srgb,currentColor 18%,transparent);background:currentColor}' +
    '.ov{font-weight:600;font-size:15px}' +
    '.ttl{font-size:12px;color:var(--sub);margin-inline-start:auto;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}' +
    '.grp{font-size:11px;font-weight:600;letter-spacing:.06em;text-transform:uppercase;color:var(--sub);margin:12px 0 2px}' +
    '.row{display:flex;align-items:center;gap:10px;padding:9px 0;border-top:1px solid var(--line)}' +
    '.grp+.row{border-top:0}' +
    '.flag{width:20px;height:15px;border-radius:3px;flex:none;object-fit:cover;box-shadow:0 0 0 1px rgba(0,0,0,.08)}' +
    '.nf{width:20px;flex:none}' +
    '.name{flex:1;min-width:0;font-weight:500;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}' +
    '.bars{display:flex;gap:2px;height:18px;align-items:stretch;direction:ltr;flex:none}' +
    '.bars i{width:4px;border-radius:2px;background:var(--empty)}' +
    '.up{font-size:12px;color:var(--sub);min-width:52px;text-align:end;font-variant-numeric:tabular-nums;flex:none}' +
    '.st{display:inline-flex;align-items:center;gap:6px;font-size:12.5px;font-weight:500;white-space:nowrap;flex:none;min-width:84px;justify-content:flex-end}' +
    '.st b{width:7px;height:7px;border-radius:50%;background:currentColor}' +
    '.foot{display:flex;justify-content:space-between;gap:8px;margin-top:10px;font-size:11px;color:var(--sub)}' +
    '.foot a{color:inherit;text-decoration:none}.foot a:hover{text-decoration:underline}' +
    '.msg{color:var(--sub);font-size:13px;padding:6px 0}' +
    '@container (max-width:420px){.bars{display:none}}' +
    '@media (max-width:480px){.bars{display:none}.st{min-width:0}}'

  var wrap = document.createElement('div')
  var style = document.createElement('style')
  style.textContent = css
  root.appendChild(style)
  root.appendChild(wrap)
  function frame(dir) {
    wrap.className = 'w ' + (themeOpt === 'dark' ? 'dark' : themeOpt === 'light' ? 'light' : 'auto')
    wrap.setAttribute('dir', dir)
    wrap.setAttribute('lang', lang)
  }
  frame(lang === 'fa' ? 'rtl' : 'ltr')
  wrap.innerHTML = '<div class="msg">' + T[lang].loading + '</div>'

  if (!slug || !key) {
    wrap.innerHTML = '<div class="msg">Neo Uptime widget: data-slug and data-key are required.</div>'
    return
  }

  function render(d) {
    lang = pickLang(d.page && d.page.language)
    var t = T[lang]
    frame(lang === 'fa' ? 'rtl' : 'ltr')
    var h = '<div class="top" style="color:' + (overallColor[d.overall] || 'var(--mute)') + '"><span class="dot"></span>' +
      '<span class="ov" style="color:var(--fg)">' + esc(t[d.overall] || t.unknown) + '</span>' +
      (showTitle && d.page && d.page.title ? '<span class="ttl">' + esc(d.page.title) + '</span>' : '') + '</div>'
    var groups = d.groups || []
    for (var g = 0; g < groups.length; g++) {
      var grp = groups[g]
      if (!grp.monitors || !grp.monitors.length) continue
      if (grp.name && groups.length > 1) h += '<div class="grp">' + esc(grp.name) + '</div>'
      for (var i = 0; i < grp.monitors.length; i++) {
        var m = grp.monitors[i]
        var flag = /^[a-z]{2}(-[a-z]+)?$/.test(m.country || '')
          ? '<img class="flag" alt="" loading="lazy" src="' + server + '/flags/' + m.country + '.svg">'
          : '<span class="nf"></span>'
        var bars = ''
        if (showHistory) {
          var hist = (m.history || []).slice(-30)
          for (var k = hist.length; k < 30; k++) bars += '<i></i>'
          for (var j = 0; j < hist.length; j++) bars += '<i style="background:' + (bar[hist[j]] || 'var(--empty)') + '"></i>'
          bars = '<span class="bars" aria-hidden="true">' + bars + '</span>'
        }
        var c = colors[m.status] || 'var(--mute)'
        var lat = m.latencyMs != null && m.status === 'up' ? num(Math.round(m.latencyMs)) + ' ' + t.ms : ''
        h += '<div class="row" title="' + esc(m.message || lat) + '">' + flag + '<span class="name">' + esc(m.name) + '</span>' + bars +
          '<span class="up" title="' + t.uptime + ' 24h">' + pct(m.uptime24h) + '</span>' +
          '<span class="st" style="color:' + c + '"><b></b>' + t[word[m.status] || 'none'] + '</span></div>'
      }
    }
    var when = new Date(d.updatedAt || Date.now())
    var time
    try {
      time = when.toLocaleTimeString(lang === 'fa' ? 'fa-IR' : 'en-GB', { hour: '2-digit', minute: '2-digit' })
    } catch (e) {
      time = when.toTimeString().slice(0, 5)
    }
    h += '<div class="foot"><span>' + t.updated + ' ' + time + '</span><a href="' + server + '/status/' + encodeURIComponent(slug) +
      '" target="_blank" rel="noopener">Neo Uptime ↗</a></div>'
    wrap.innerHTML = h
  }

  var failures = 0
  function load() {
    var url = server + '/api/v1/status/' + encodeURIComponent(slug)
    fetch(url, { headers: { Authorization: 'Bearer ' + key }, mode: 'cors', credentials: 'omit', cache: 'no-store' })
      .then(function (r) {
        if (!r.ok) throw new Error('HTTP ' + r.status)
        return r.json()
      })
      .then(function (d) {
        failures = 0
        render(d)
      })
      .catch(function (e) {
        failures++
        if (failures === 1 && wrap.querySelector('.row')) return // keep the last good view through one blip
        wrap.innerHTML = '<div class="msg">' + T[lang].error + '</div>'
        if (window.console) console.warn('Neo Uptime widget:', e && e.message)
      })
  }
  load()
  setInterval(function () {
    if (!document.hidden) load()
  }, refresh)
  document.addEventListener('visibilitychange', function () {
    if (!document.hidden) load()
  })
})()
