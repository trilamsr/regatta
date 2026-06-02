// htmx-config.js — sourced after htmx.min.js (defer) so the global is live.
// allowEval=false closes the CSP-strict gate spec §3.5 mandates; selfRequestsOnly=true
// keeps every hx-* request same-origin so a stolen template attribute cannot exfiltrate.
htmx.config.allowEval = false;
htmx.config.selfRequestsOnly = true;
