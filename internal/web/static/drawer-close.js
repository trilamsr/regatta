// drawer-close.js — closes the slide-in drawer on backdrop or close-button click.
// Event delegation (vs hx-on:click) avoids htmx:evalDisallowedError under the
// CSP-strict + allowEval=false posture (MAY-57).
document.addEventListener("click", function (e) {
  const t = e.target;
  if (!(t instanceof Element)) return;
  if (!t.closest(".drawer-close, .drawer-bg")) return;
  const mount = document.getElementById("drawer-mount");
  if (mount) mount.innerHTML = "";
});
