#!/usr/bin/env bash
#
# Keychain wrapper for the regatta LaunchAgent.
#
# The shipped com.regatta.serve.plist invokes /usr/local/bin/regatta
# directly so the install path works for operators who source secrets
# from elsewhere (1Password CLI, sops, env file). When tokens live in
# the macOS keychain, replace ProgramArguments[0] in the plist with the
# path to this wrapper.
#
# Install:
#   sudo install -m 0755 -o root -g wheel \
#     deploy/launchd/regatta-serve.sh /usr/local/bin/regatta-serve
#
# Stash secrets (one-time):
#   security add-generic-password -a "$USER" -s regatta/anthropic_api_key -w
#   security add-generic-password -a "$USER" -s regatta/gh_token -w
#
# Point the plist at the wrapper, then reload:
#   sed -i '' 's|/usr/local/bin/regatta|/usr/local/bin/regatta-serve|' \
#     "$HOME/Library/LaunchAgents/com.regatta.serve.plist"
#   launchctl bootout "gui/$(id -u)/com.regatta.serve"
#   launchctl bootstrap "gui/$(id -u)" \
#     "$HOME/Library/LaunchAgents/com.regatta.serve.plist"

set -euo pipefail

# security(1) `-w` writes ONLY the secret to stdout. If the keychain
# entry is missing, fail fast with a clear message — silently exec'ing
# regatta with empty credentials would crash-loop with an opaque error.
read_keychain() {
  local service="$1"
  if ! security find-generic-password -a "$USER" -s "$service" -w 2>/dev/null; then
    echo "regatta-serve: keychain entry '$service' missing for user '$USER'" >&2
    echo "regatta-serve: run 'security add-generic-password -a \$USER -s $service -w' to stash it" >&2
    exit 1
  fi
}

export ANTHROPIC_API_KEY="$(read_keychain regatta/anthropic_api_key)"
export GH_TOKEN="$(read_keychain regatta/gh_token)"

exec /usr/local/bin/regatta "$@"
