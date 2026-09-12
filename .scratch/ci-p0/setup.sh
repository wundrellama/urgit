#!/bin/bash
# Harness step 2: create repo ci-fixture with one commit on master, then push
# a second commit from a local clone over Smart HTTP (lands: not CI-protected
# yet). Records OIDs in $TMP/oids.env.
source "$(dirname "$0")/env.sh"
api="$ROOT/.scratch/ci-p0/api.sh"
echo "== create repository"
"$api" POST /repositories '{"name":"ci-fixture","publicRead":true}'
echo "== protect refs/heads/master (today's fast-forward rule)"
"$api" POST /repository/ci-fixture/protected '{"ref":"refs/heads/master","protected":true}'
A="$TMP/clone-a"
rm -rf "$A"; mkdir -p "$A/.github/workflows"; cd "$A"
git init -q -b master .
git config user.name harness; git config user.email harness@example
git config http.cookieFile "$JAR"
git remote add origin "$URL/git/ci-fixture"
cp "$ROOT/desk/tests/ci/fixture-pass.yml" .github/workflows/fixture-pass.yml
cp "$ROOT/desk/tests/ci/fixture-fail.yml" .github/workflows/fixture-fail.yml
echo "one" > README.md
git add -A && git commit -qm "one: workflows and readme"
ONE=$(git rev-parse HEAD)
echo "== push commit one"
git push origin master 2>&1 | tail -3
echo "two" >> README.md
git add -A && git commit -qm "two: second commit"
TWO=$(git rev-parse HEAD)
echo "== push commit two (not CI-protected yet: must land)"
git push origin master 2>&1 | tail -3
echo "== remote refs"
"$api" GET /repository/ci-fixture | sed 's/\("refs":\[[^]]*\]\).*/\1/' | cut -c1-400
cat > "$TMP/oids.env" <<EOF
export ONE=$ONE
export TWO=$TWO
EOF
echo "ONE=$ONE TWO=$TWO"
