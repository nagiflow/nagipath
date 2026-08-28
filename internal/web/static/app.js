// Keyboard shortcuts, and nothing else. This audience lives on keyboards, so
// these are not a power-user extra; every other behaviour on the page is htmx
// or plain HTML.
//
// ponytail: no palette, no fuzzy matcher — "/" focuses the real search field
// that already submits to /search. Build a palette when a real search-across-
// everything endpoint exists to back it.
(function () {
  "use strict";

  var typing = function (el) {
    return el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" ||
      el.tagName === "SELECT" || el.isContentEditable);
  };

  // g then t/r/s/n/i/c/d/k — the second key within 800ms, EUI/Gmail style.
  var jumps = {
    t: "/trace", r: "/rules", s: "/search", n: "/nodes",
    i: "/instances", c: "/certificates", d: "/drift", k: "/clusters",
  };
  var pending = 0;

  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") {
      if (typing(document.activeElement)) document.activeElement.blur();
      var open = document.querySelector("details[data-panel][open]");
      if (open) open.removeAttribute("open");
      return;
    }
    if (e.metaKey || e.ctrlKey) {
      if (e.key === "k") { e.preventDefault(); focusSearch(); }
      return;
    }
    if (e.altKey || typing(e.target)) return;

    if (e.key === "/") { e.preventDefault(); focusSearch(); return; }
    if (e.key === "g") { pending = Date.now(); return; }
    if (pending && Date.now() - pending < 800 && jumps[e.key]) {
      pending = 0;
      window.location.href = jumps[e.key];
      return;
    }
    pending = 0;
  });

  function focusSearch() {
    var el = document.getElementById("gsearch");
    if (el) { el.focus(); el.select(); }
  }

  // [data-copy="<selector>"] copies that element's text. One delegated listener
  // rather than an onclick per button: the CSP has no script-src 'unsafe-inline',
  // so an inline handler is not merely discouraged here, it does not run.
  document.addEventListener("click", function (e) {
    var btn = e.target.closest && e.target.closest("[data-copy]");
    if (!btn) return;
    var src = document.querySelector(btn.getAttribute("data-copy"));
    if (!src || !navigator.clipboard) return;
    // A numbered code block keeps its line numbers in the DOM, so innerText
    // pasted "1# /etc/sudoers.d/nagipath" — a sudoers file that does not parse.
    // The gutter is one span, the line is the next, and only the line is copied.
    var rows = src.querySelectorAll(".cl");
    var text = src.innerText;
    if (rows.length) {
      text = Array.prototype.map.call(rows, function (row) {
        var cells = row.children;
        return cells.length > 1 ? cells[cells.length - 1].innerText : row.innerText;
      }).join("\n");
    }
    navigator.clipboard.writeText(text).then(function () {
      // The label is the only feedback. A copy that silently did nothing is
      // worse than no button, so failure leaves the label alone.
      var was = btn.textContent;
      btn.textContent = "Copied";
      setTimeout(function () { btn.textContent = was; }, 1200);
    });
  });

  document.addEventListener("submit", function (e) {
    var form = e.target.closest && e.target.closest("form[data-confirm]");
    if (form && !window.confirm(form.getAttribute("data-confirm"))) e.preventDefault();
  });

  // Credential types share one form. Toggling fields here keeps the normal
  // POST endpoint simple and leaves the non-JavaScript fallback understandable.
  var credentialForm = document.querySelector("[data-credential-form]");
  if (credentialForm) {
    var credentialType = credentialForm.querySelector("[data-credential-type]");
    var show = function (selector, visible) {
      Array.prototype.forEach.call(credentialForm.querySelectorAll(selector), function (el) {
        el.hidden = !visible;
      });
    };
    var syncCredentialType = function () {
      var type = credentialType.value;
      var key = type === "private_key" || type === "ssh_certificate";
      var password = type === "username_password" || type === "ldap" || type === "kerberos";
      var cyberark = type === "cyberark";
      show("[data-credential-key]", key);
      show("[data-credential-certificate]", type === "ssh_certificate");
      show("[data-credential-password]", password);
      show("[data-credential-reference]", cyberark);
      credentialForm.querySelector("[data-credential-key-input]").required = key;
      credentialForm.querySelector("[data-credential-certificate-input]").required = type === "ssh_certificate";
      credentialForm.querySelector("[data-credential-password-input]").required = password;
      credentialForm.querySelector("[data-credential-reference-input]").required = cyberark;
      credentialForm.querySelector("[data-credential-username]").required = !cyberark;
    };
    credentialType.addEventListener("change", syncCredentialType);
    syncCredentialType();
  }
})();
