#!/bin/bash
# Row R12 (BRIEF-CI-P3 D8, S6): the README is the operator's setup guide.
# Every step's command or click exists in the tree it ships with — the
# sections in the brief's order, each button the guide names present in
# the Runners section or the CI tab, each TOML key the guide names read by
# the daemon's config, each daemon log line the guide quotes emitted by
# the daemon, the three install commands, the refusal strings the guide
# quotes emitted by the ship; no dojo instruction remains; the
# reachability sentence is present in bold; the guide is bundled into the
# web app so the CI tab's link opens it.
source "$(dirname "$0")/lib.sh"
row "R12: runner/README.md is the setup guide: every step's command or click exists; no dojo step; the reachability sentence is there"
README="$ROOT/runner/README.md"
check "no ':urgit-ci|' dojo instruction (grep -c)" "0" "$(grep -c ':urgit-ci|' "$README")"
check "no dojo instruction at all (':urgit-ci &' or 'dojo>')" "0" "$(grep -cE ':urgit-ci &|dojo>' "$README")"
check "the sections come in the brief's order" "1 2 3 4 5 6 7" "$(grep -oE '^## [1-7]\. ' "$README" | awk '{print $2}' | tr -d . | tr '\n' ' ' | sed 's/ $//')"
check "the section titles" "Object store|Mint a token|Install the daemon|Protect a branch|Watch|Runners|Limitations in this release" "$(grep -oE '^## [1-7]\. .*' "$README" | sed 's/^## [1-7]\. //' | tr '\n' '|' | sed 's/|$//')"
check "the reachability sentence, in bold" "1" "$(grep -cF "**The endpoint must be reachable from every browser that will view logs, not only from the ship's host.**" "$README")"
check "the localhost/LAN symptom sentence" "1" "$(grep -c "A \`localhost\` or LAN-only endpoint works for the runner and breaks the log view for everyone else, and the symptom is \`Log unavailable: NetworkError\`" "$README")"
check "GroundSeg named once, nothing else of the operator's infrastructure" "1 0" "$(grep -c GroundSeg "$README") $(grep -ciE 'tellurian|startram|herdr|chair|ruling|CI-P3|rider' "$README")"
# the clicks: every button the guide names exists in the web app's source
for button in "Mint token" "Expire" "Revoke" "Remove" "Rotate CI key" "Refresh" "Approve and run trusted" "Re-run" "CI required"; do
  check "the guide's '$button' is a control in the app" "yes" "$(grep -qF "$button" "$README" && grep -rqF "$button" "$ROOT/fe/src/components/Runners.jsx" "$ROOT/fe/src/components/CiTab.jsx" "$ROOT/fe/src/components/RepositoryView.jsx" && echo yes || echo no)"
done
check "the guide's 'Settings → Runners' section exists" "yes" "$(grep -qF "Settings → Runners" "$README" && grep -qF "<h3>Runners</h3>" "$ROOT/fe/src/components/Runners.jsx" && echo yes || echo no)"
# the TOML keys the guide's table names are the daemon's config keys
for key in ship_url enroll_token sandbox docker_host act_binary act_image capacity labels work_dir state_file; do
  check "TOML key $key: in the guide and read by the daemon" "yes" "$(grep -qF "\`$key\`" "$README" && grep -qF "toml:\"$key\"" "$ROOT/runner/internal/config/config.go" && echo yes || echo no)"
done
# the commands: the build line and the three install lines
check "the build command" "1" "$(grep -cF 'cd runner && CGO_ENABLED=0 go build -o urgit-runner ./cmd/urgit-runner' "$README")"
check "the build command builds" "yes" "$( (cd "$ROOT/runner" && CGO_ENABLED=0 go build -o "$TMP/r12-runner" ./cmd/urgit-runner) && echo yes || echo no)"
for cmd in 'install -m 0755 urgit-runner /usr/local/bin/urgit-runner' 'install -m 0600 urgit-runner.toml /etc/urgit-runner.toml' 'systemctl enable --now urgit-runner'; do
  check "install command: $cmd" "1" "$(grep -cF "$cmd" "$README")"
done
check "the unit file the guide names exists" "yes" "$([ -f "$ROOT/runner/urgit-runner.service" ] && echo yes || echo no)"
check "the example config the guide names exists and carries labels" "yes" "$(grep -qE '^labels = ' "$ROOT/runner/urgit-runner.toml.example" && echo yes || echo no)"
# the strings the guide quotes are the product's
for s in "revoked by the ship" "runner went silent" "attempt is closed" "no runner has labels" "ship object storage is not configured; CI cannot be enabled" "ref has no tip; push a commit before CI-protecting it" "destination moved; rebase and push again" "staged as ci candidate" "docker-rootless (container isolation; microvm backend pending)"; do
  check "quoted string exists in the product: '$s'" "yes" "$(grep -qF "$s" "$README" && grep -rqF "$s" "$ROOT/desk/app" "$ROOT/runner/internal" "$ROOT/runner/cmd" && echo yes || echo no)"
done
check "the implicit label set the guide names is the ship's" "yes" "$(for l in self-hosted linux ubuntu-latest ubuntu-22.04 ubuntu-24.04 x64; do grep -qF "\`$l\`" "$README" && grep -qF "'$l'" "$ROOT/desk/app/urgit-ci.hoon" || echo no; done | grep -q no && echo no || echo yes)"
check "the guide is bundled into the app (the CI tab's link opens it)" "yes" "$(grep -qF "runner/README.md?raw" "$ROOT/fe/src/components/SetupGuide.jsx" && grep -qF "SetupGuide" "$ROOT/fe/src/components/CiTab.jsx" && echo yes || echo no)"
check "the built app carries the guide's reachability sentence" "1" "$(grep -c "The endpoint must be reachable from every browser that will view logs" "$ROOT/desk/web/app.js" 2>/dev/null | sed 's/^[2-9][0-9]*$/1/')"
check "the limitations section keeps microvm as the next phase" "1" "$(grep -c 'microvm' "$README" | sed 's/^[2-9]$/1/')"
end_row R12
[ "$NFAIL" = 0 ]
