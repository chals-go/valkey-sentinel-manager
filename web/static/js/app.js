// === Event Delegation ===

// Click delegation: modal open/close, copy, detail toggle, token toggle, refresh toggle
document.addEventListener('click', function(e) {
  // Modal open
  var open = e.target.closest('[data-modal-open]');
  if (open) { openModal(open.getAttribute('data-modal-open')); return; }

  // Modal close
  var close = e.target.closest('[data-modal-close]');
  if (close) { closeModal(close.getAttribute('data-modal-close')); return; }

  // Stop propagation
  if (e.target.closest('[data-stop-propagation]')) { e.stopPropagation(); return; }

  // Copy all groups
  var copyAll = e.target.closest('[data-copy-all]');
  if (copyAll) { copyAllGroups(copyAll); return; }

  // Copy to clipboard (uses existing data-copy attribute)
  var copy = e.target.closest('[data-copy-btn]');
  if (copy) { e.stopPropagation(); copyToClipboard(copy); return; }

  // Toggle detail row
  var detail = e.target.closest('[data-toggle-detail]');
  if (detail) { var el = document.getElementById(detail.getAttribute('data-toggle-detail')); if (el) el.toggleAttribute('open'); return; }

  // Select all text in input
  var sel = e.target.closest('[data-select-all]');
  if (sel) { sel.select(); return; }

  // Toggle token show/hide
  var tok = e.target.closest('[data-toggle-token]');
  if (tok) {
    var name = tok.getAttribute('data-toggle-token');
    var f = document.getElementById('token-' + name);
    if (f) {
      if (f.type === 'password') { f.type = 'text'; tok.textContent = tok.getAttribute('data-label-hide'); }
      else { f.type = 'password'; tok.textContent = tok.getAttribute('data-label-show'); }
    }
    return;
  }

  // Auto-refresh toggle
  var rt = e.target.closest('[data-refresh-toggle]');
  if (rt) {
    if (autoRefreshTimer) { stopAutoRefresh(); }
    else { setRefreshInterval(parseInt(document.getElementById('refresh-select').value) || 5); }
    return;
  }
});

// Submit delegation: confirm dialog
document.addEventListener('submit', function(e) {
  var form = e.target.closest('[data-confirm]');
  if (form && !confirm(form.getAttribute('data-confirm'))) { e.preventDefault(); }
});

// Change delegation: refresh interval, DNS preview, provider/azure toggles, sentinel inputs
document.addEventListener('change', function(e) {
  // Refresh interval
  if (e.target.closest('[data-refresh-interval]')) { setRefreshInterval(parseInt(e.target.value)); return; }
  // DNS preview (new cluster modal)
  if (e.target.closest('[data-dns-preview-new]')) { if (typeof updateNewDnsPreview === 'function') updateNewDnsPreview(); return; }
  // DNS preview (edit page — existing DNS)
  if (e.target.closest('[data-dns-preview]')) { if (typeof updateDnsPreview === 'function') updateDnsPreview(); return; }
  // DNS preview (add DNS endpoint — edit modal/page)
  var editPrev = e.target.closest('[data-edit-dns-preview]');
  if (editPrev) {
    var mn = editPrev.getAttribute('data-edit-dns-preview');
    var opt = editPrev.options[editPrev.selectedIndex];
    var z = opt ? opt.getAttribute('data-zone') || '' : '';
    var sfx = z ? '.' + z : '';
    var pp = document.querySelector('[data-preview-primary="' + mn + '"]');
    var pr = document.querySelector('[data-preview-replica="' + mn + '"]');
    if (pp) pp.textContent = 'primary-' + mn + sfx;
    if (pr) pr.textContent = 'replica-' + mn + sfx;
    return;
  }
  // Toggle replica preview (add DNS endpoint)
  var editRepToggle = e.target.closest('[data-edit-replica-toggle]');
  if (editRepToggle) {
    var mn2 = editRepToggle.getAttribute('data-edit-replica-toggle');
    var row = document.querySelector('[data-preview-replica-row="' + mn2 + '"]');
    if (row) row.classList.toggle('hidden', !editRepToggle.checked);
    return;
  }
  // Toggle replica preview (register modal)
  if (e.target.closest('[data-toggle-replica]')) { if (typeof toggleReplicaPreview === 'function') toggleReplicaPreview(); return; }
  // Sentinel inputs count
  if (e.target.closest('[data-sentinel-inputs]')) { if (typeof updateModalSentinelInputs === 'function') updateModalSentinelInputs(); return; }
  // Toggle provider fields (new DNS modal)
  if (e.target.closest('[data-toggle-provider]')) { if (typeof toggleModalProviderFields === 'function') toggleModalProviderFields(e.target.value); return; }
  // Toggle azure auth (new DNS modal)
  if (e.target.closest('[data-toggle-azure-auth]')) { if (typeof toggleModalAzureAuth === 'function') toggleModalAzureAuth(e.target.value); return; }
  // Toggle edit azure auth (DNS edit modals, per-provider)
  var editAz = e.target.closest('[data-toggle-edit-az-auth]');
  if (editAz) { if (typeof toggleEditAzAuth === 'function') toggleEditAzAuth(editAz.getAttribute('data-toggle-edit-az-auth'), e.target.value); return; }
  // Toggle R53 keys checkbox
  if (e.target.closest('[data-toggle-r53-keys]')) { var fld = document.getElementById('modal-r53-key-fields'); if (fld) fld.style.display = e.target.checked ? '' : 'none'; return; }
  // Toggle edit azure auth (dns_edit page)
  if (e.target.closest('[data-toggle-edit-azure-auth]')) { if (typeof toggleEditAzureAuth === 'function') toggleEditAzureAuth(e.target.value); return; }
  // Skip DNS toggle
  var skipDns = e.target.closest('[data-toggle-skip-dns]');
  if (skipDns) {
    var off = skipDns.checked;
    var form = skipDns.closest('form');
    var provider = form.querySelector('[name="dns_provider"]');
    var ttl = form.querySelector('[name="dns_ttl"]');
    var replicaDns = form.querySelector('[name="create_replica_dns"]');
    var preview = form.querySelector('[data-dns-preview-area]');
    [provider, ttl].forEach(function(el) {
      if (el) { el.disabled = off; el.classList.toggle('opacity-50', off); el.classList.toggle('bg-gray-100', off); }
    });
    if (replicaDns) { replicaDns.disabled = off; replicaDns.closest('label').classList.toggle('opacity-50', off); }
    if (preview) { preview.classList.toggle('hidden', off); }
    if (provider) { provider.required = !off; }
    return;
  }
  // Add DNS endpoint toggle (edit page/modal, DNS disabled clusters)
  var addDns = e.target.closest('[data-toggle-add-dns]');
  if (addDns) {
    var form = addDns.closest('form') || addDns.closest('.modal-overlay');
    var fields = form ? form.querySelector('[data-dns-add-fields]') : document.getElementById('edit-dns-fields');
    if (fields) { fields.classList.toggle('hidden', !addDns.checked); }
    if (fields) {
      var prov = fields.querySelector('[name="dns_provider"]');
      if (prov) prov.required = addDns.checked;
    }
    return;
  }
  // Add Replica DNS toggle
  var addReplica = e.target.closest('[data-toggle-add-replica]');
  if (addReplica) {
    var preview = addReplica.closest('form') || addReplica.closest('.modal-overlay');
    var pv = preview ? preview.querySelector('[data-replica-preview]') : null;
    if (pv) pv.classList.toggle('hidden', !addReplica.checked);
    return;
  }
  // Disable DNS toggle
  var disDns = e.target.closest('[data-toggle-disable-dns]');
  if (disDns) {
    var frm = disDns.closest('form');
    if (!frm) return;
    var els = frm.querySelectorAll('[name="dns_provider"], [name="dns_ttl"], [name="primary_record"], [name="replica_domain"], [name="replica_ttl"], [name="add_replica_dns"]');
    for (var i = 0; i < els.length; i++) { els[i].disabled = disDns.checked; }
    frm.querySelectorAll('[data-replica-preview]').forEach(function(el) { if (disDns.checked) el.classList.add('hidden'); });
    return;
  }
});

// Input delegation: DNS preview
document.addEventListener('input', function(e) {
  if (e.target.closest('[data-dns-preview]')) { if (typeof updateDnsPreview === 'function') updateDnsPreview(); }
});

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
