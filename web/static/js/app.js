// Modal
function openModal(id) {
  var el = document.getElementById(id);
  if (el) {
    el.classList.add('open');
    document.body.style.overflow = 'hidden';
  }
}
function closeModal(id) {
  var el = document.getElementById(id);
  if (el) {
    el.classList.remove('open');
    document.body.style.overflow = '';
  }
}

// Close on overlay click
document.addEventListener('click', function(e) {
  if (e.target.classList.contains('modal-overlay')) {
    e.target.classList.remove('open');
    document.body.style.overflow = '';
  }
});

// Close on ESC key
document.addEventListener('keydown', function(e) {
  if (e.key === 'Escape') {
    document.querySelectorAll('.modal-overlay.open').forEach(function(m) {
      m.classList.remove('open');
    });
    document.body.style.overflow = '';
  }
});

// Settings submenu toggle
function toggleSubMenu(id) {
  var el = document.getElementById(id);
  if (el) el.classList.toggle('open');
}

// === Auto Refresh ===
var autoRefreshTimer = null;
var autoRefreshSeconds = 5;

function startAutoRefresh(seconds) {
  stopAutoRefresh();
  autoRefreshSeconds = seconds;
  autoRefreshTimer = setInterval(function() {
    // Skip if a modal is open
    if (document.querySelector('.modal-overlay.open')) return;
    location.reload();
  }, seconds * 1000);
  localStorage.setItem('autoRefreshSeconds', seconds);
  localStorage.setItem('autoRefreshEnabled', 'true');
  updateRefreshUI();
}

function stopAutoRefresh() {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer);
    autoRefreshTimer = null;
  }
  localStorage.setItem('autoRefreshEnabled', 'false');
  updateRefreshUI();
}

function setRefreshInterval(seconds) {
  if (seconds <= 0) {
    stopAutoRefresh();
  } else {
    startAutoRefresh(seconds);
  }
}

function updateRefreshUI() {
  var toggle = document.getElementById('refresh-toggle');
  var select = document.getElementById('refresh-select');
  var dot = document.getElementById('refresh-dot');
  if (toggle) {
    toggle.textContent = autoRefreshTimer ? 'ON' : 'OFF';
    toggle.className = autoRefreshTimer
      ? 'px-2 py-1 text-xs font-medium rounded-md bg-emerald-100 text-emerald-700 cursor-pointer'
      : 'px-2 py-1 text-xs font-medium rounded-md bg-gray-100 text-gray-500 cursor-pointer';
  }
  if (dot) {
    dot.className = autoRefreshTimer
      ? 'w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse'
      : 'w-1.5 h-1.5 rounded-full bg-gray-300';
  }
  if (select) {
    select.value = autoRefreshTimer ? autoRefreshSeconds : 0;
  }
}

// Restore from localStorage on page load
document.addEventListener('DOMContentLoaded', function() {
  var ctrl = document.getElementById('refresh-control');
  if (!ctrl) return;

  var enabled = localStorage.getItem('autoRefreshEnabled');
  var seconds = parseInt(localStorage.getItem('autoRefreshSeconds')) || 5;
  autoRefreshSeconds = seconds;

  if (enabled === 'true' && seconds > 0) {
    startAutoRefresh(seconds);
  } else {
    updateRefreshUI();
  }
});

// === Clipboard Copy ===
function fallbackCopy(text) {
  var ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.left = '-9999px';
  document.body.appendChild(ta);
  ta.select();
  try { document.execCommand('copy'); } catch(e) {}
  document.body.removeChild(ta);
}

function doCopy(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).catch(function() { fallbackCopy(text); });
  } else {
    fallbackCopy(text);
  }
}

function showCopyFeedback(btn) {
  var copyIcon = btn.querySelector('.copy-icon');
  var checkIcon = btn.querySelector('.check-icon');
  if (copyIcon && checkIcon) {
    copyIcon.classList.add('hidden');
    checkIcon.classList.remove('hidden');
    setTimeout(function() {
      copyIcon.classList.remove('hidden');
      checkIcon.classList.add('hidden');
    }, 1500);
  }
}

function copyToClipboard(btn) {
  var text = btn.getAttribute('data-copy');
  if (!text) return;

  var parts = text.split(' | ');
  var headers = ['NAME', 'PRIMARY IP', 'REPLICA IP', 'ENDPOINT'];
  var widths = headers.map(function(h, i) {
    var cellLen = (parts[i] || '').length;
    return Math.max(h.length, cellLen);
  });
  function pad(str, len) { str = str || ''; while (str.length < len) str += ' '; return str; }

  var sep = '| ' + widths.map(function(w) { return '-'.repeat(w); }).join(' | ') + ' |';
  var headerLine = '| ' + headers.map(function(h, i) { return pad(h, widths[i]); }).join(' | ') + ' |';
  var dataLine = '| ' + parts.map(function(cell, i) { return pad(cell || '', widths[i]); }).join(' | ') + ' |';

  doCopy([headerLine, sep, dataLine].join('\n'));
  showCopyFeedback(btn);
}

function copyAllGroups(btn) {
  var rows = document.querySelectorAll('[data-copy-row]');
  var data = [];
  rows.forEach(function(el) {
    var parts = el.getAttribute('data-copy-row').split(' | ');
    data.push(parts);
  });
  if (data.length === 0) return;

  var headers = ['NAME', 'PRIMARY IP', 'REPLICA IP', 'ENDPOINT'];
  var widths = headers.map(function(h, i) {
    var max = h.length;
    data.forEach(function(row) {
      if (row[i] && row[i].length > max) max = row[i].length;
    });
    return max;
  });

  function pad(str, len) {
    str = str || '';
    while (str.length < len) str += ' ';
    return str;
  }

  var sep = '| ' + widths.map(function(w) { return '-'.repeat(w); }).join(' | ') + ' |';
  var headerLine = '| ' + headers.map(function(h, i) { return pad(h, widths[i]); }).join(' | ') + ' |';
  var lines = [headerLine, sep];
  data.forEach(function(row) {
    lines.push('| ' + row.map(function(cell, i) { return pad(cell || '', widths[i]); }).join(' | ') + ' |');
  });

  doCopy(lines.join('\n'));
  showCopyFeedback(btn);
}
