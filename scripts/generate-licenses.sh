#!/usr/bin/env bash
# Regenerates THIRD_PARTY_LICENSES.md and web/src/lib/credits.json from the
# current dependency manifests. Idempotent; safe to run any time.
#
# Usage: ./scripts/generate-licenses.sh
#
# Tools (no global install required):
#   - github.com/google/go-licenses          (Go modules)
#   - license-checker-rseidelsohn (via npx)  (npm packages)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OUT_MD="$ROOT/THIRD_PARTY_LICENSES.md"
OUT_JSON="$ROOT/web/src/lib/credits.json"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "→ Collecting Go module licenses (api + worker)"
(
  cd "$ROOT/api"
  go run github.com/google/go-licenses/v2@v2.0.1 csv \
    ./cmd/api ./cmd/worker \
    --ignore github.com/julian-alarcon/dothesplit/api \
    2>"$TMP/go-licenses.err" \
    | sort -u >"$TMP/go.csv"
)

echo "→ Collecting npm package licenses (web)"
(
  cd "$ROOT/web"
  npx --yes license-checker-rseidelsohn@4.4.2 \
    --json --start . \
    --excludePackages "dothesplit-web@$(jq -r .version package.json)" \
    >"$TMP/npm.json" 2>"$TMP/npm.err"
)

# --- credits.json (curated top-level deps for the /credits page) -----------

echo "→ Building credits.json (curated top-level deps)"

# Direct backend deps from go.mod (exclude `// indirect` and the `tool` line).
GO_DIRECT="$(awk '
  /^require \(/ { in_block=1; next }
  /^\)/ && in_block { in_block=0; next }
  in_block && !/indirect/ && NF>=2 { print $1 }
' "$ROOT/api/go.mod")"

# Map module name → license by matching go.csv prefixes (longest-prefix wins).
# We iterate licenses descending by path length so e.g. github.com/foo/bar/sub
# matches before github.com/foo/bar.
go_license_for() {
  local mod="$1"
  awk -F, -v m="$mod" '
    { print length($1), $0 }
  ' "$TMP/go.csv" | sort -rn | while IFS= read -r line; do
    pathlen="${line%% *}"
    rest="${line#* }"
    pkgpath="${rest%%,*}"
    case "$pkgpath" in
      "$mod"|"$mod"/*)
        rest_after_path="${rest#*,}"
        url="${rest_after_path%,*}"
        license="${rest_after_path##*,}"
        printf '%s\t%s\n' "$license" "$url"
        return 0
        ;;
    esac
  done
  printf 'UNKNOWN\t\n'
}

# Build backend array as JSON.
backend_json="["
first=1
while IFS= read -r mod; do
  [ -z "$mod" ] && continue
  # Resolve version from go.mod
  version=$(awk -v m="$mod" '$1==m { print $2; exit }' "$ROOT/api/go.mod")
  IFS=$'\t' read -r license url < <(go_license_for "$mod")
  # Fall back: derive URL from module name if go-licenses didn't find a match.
  if [ -z "$url" ]; then
    case "$mod" in
      github.com/*) url="https://$mod" ;;
      golang.org/x/*) url="https://pkg.go.dev/$mod" ;;
      *) url="https://pkg.go.dev/$mod" ;;
    esac
  else
    # Strip /blob/... suffix for a cleaner home URL.
    url="${url%%/blob/*}"
  fi
  [ $first -eq 0 ] && backend_json+=","
  first=0
  backend_json+=$(jq -nc --arg n "$mod" --arg v "$version" --arg l "$license" --arg u "$url" \
    '{name:$n,version:$v,license:$l,url:$u}')
done <<<"$GO_DIRECT"
backend_json+="]"

# Direct frontend deps from package.json (dependencies + devDependencies).
FRONTEND_PKGS="$(jq -r '
  ((.dependencies // {}) + (.devDependencies // {}))
  | to_entries[]
  | "\(.key)\t\(.value)"
' "$ROOT/web/package.json")"

# Resolve installed version + license from npm.json.
npm_meta_for() {
  local name="$1"
  jq -r --arg n "$name" '
    to_entries
    | map(select(.key | startswith($n + "@")))
    | .[0]
    | if . == null then "UNKNOWN\tUNKNOWN\t" else
        .key as $k
        | ($k | sub("^" + $n + "@"; "")) as $v
        | "\((.value.licenses // "UNKNOWN") | if type=="array" then join(" OR ") else . end)\t\($v)\t\(.value.repository // "")"
      end
  ' "$TMP/npm.json"
}

frontend_json="["
first=1
while IFS=$'\t' read -r name range; do
  [ -z "$name" ] && continue
  IFS=$'\t' read -r license version url < <(npm_meta_for "$name")
  [ -z "$url" ] && url="https://www.npmjs.com/package/$name"
  [ $first -eq 0 ] && frontend_json+=","
  first=0
  frontend_json+=$(jq -nc --arg n "$name" --arg v "$version" --arg l "$license" --arg u "$url" \
    '{name:$n,version:$v,license:$l,url:$u}')
done <<<"$FRONTEND_PKGS"
frontend_json+="]"

# Assemble final credits.json. Font Awesome CC BY 4.0 attribution block is
# the legally required bit - keep all four elements (creator, license URL,
# license page, modification statement).
mkdir -p "$(dirname "$OUT_JSON")"
jq -n \
  --argjson backend "$backend_json" \
  --argjson frontend "$frontend_json" \
  '{
    project: {
      name: "DoTheSplit",
      license: "MIT",
      copyright: "Copyright (c) 2026 Julian Alarcon",
      url: "https://github.com/julian-alarcon/dothesplit"
    },
    fontAwesome: {
      creator: "Font Awesome",
      creatorUrl: "https://fontawesome.com",
      license: "CC BY 4.0",
      licenseUrl: "https://creativecommons.org/licenses/by/4.0/",
      licensePageUrl: "https://fontawesome.com/license/free",
      modificationStatement: "Icons used unmodified."
    },
    inter: {
      creator: "Rasmus Andersson",
      creatorUrl: "https://rsms.me/inter/",
      license: "SIL Open Font License 1.1",
      licenseUrl: "https://openfontlicense.org/",
      licensePath: "web/src/assets/fonts/inter/OFL.txt",
      modificationStatement: "Files used unmodified."
    },
    backend: $backend,
    frontend: $frontend
  }' >"$OUT_JSON"

echo "  wrote $OUT_JSON ($(jq '.backend | length' "$OUT_JSON") backend, $(jq '.frontend | length' "$OUT_JSON") frontend)"

# --- THIRD_PARTY_LICENSES.md (full transitive list) ------------------------

echo "→ Writing THIRD_PARTY_LICENSES.md"

{
  cat <<'EOF'
# Third-Party Licenses

DoTheSplit depends on the following open-source software. Each project is
listed with its module/package name, version, SPDX license identifier, and
upstream URL. The full license text for every dependency is preserved in its
upstream repository (linked below) and in the `node_modules/` and Go module
cache directories of any local checkout.

This file is regenerated by `make licenses` (see
[scripts/generate-licenses.sh](scripts/generate-licenses.sh)). Do not edit by
hand.

For human-readable attribution and the Font Awesome CC BY 4.0 notice, see the
`/credits` route in the running application.

EOF

  echo "## Font Awesome Free Icons (CC BY 4.0)"
  echo
  echo "DoTheSplit uses icons from [Font Awesome](https://fontawesome.com), the Free"
  echo "tier, distributed under [Creative Commons Attribution 4.0](https://creativecommons.org/licenses/by/4.0/)."
  echo "License page: <https://fontawesome.com/license/free>. Icons are used unmodified."
  echo

  echo "## Inter Font (SIL Open Font License 1.1)"
  echo
  echo "DoTheSplit ships [Inter](https://rsms.me/inter/) by Rasmus Andersson,"
  echo "self-hosted under the [SIL Open Font License 1.1](https://openfontlicense.org/)."
  echo "License text: [web/src/assets/fonts/inter/OFL.txt](web/src/assets/fonts/inter/OFL.txt)."
  echo "Files used unmodified."
  echo

  echo "## Backend (Go modules)"
  echo
  echo "Generated from \`go-licenses csv ./cmd/api ./cmd/worker\` against [api/go.mod](api/go.mod)."
  echo
  echo "| Module | License | Source |"
  echo "|---|---|---|"
  awk -F, '{ printf "| %s | %s | %s |\n", $1, $3, $2 }' "$TMP/go.csv"
  echo

  echo "## Frontend (npm packages)"
  echo
  echo "Generated from \`license-checker-rseidelsohn --json\` against [web/package.json](web/package.json)."
  echo
  echo "| Package | License | Source |"
  echo "|---|---|---|"
  jq -r '
    to_entries
    | sort_by(.key)
    | .[]
    | "| \(.key) | \(.value.licenses // "UNKNOWN" | if type=="array" then join(" OR ") else . end) | \(.value.repository // "—") |"
  ' "$TMP/npm.json"
  echo

  cat <<'EOF'
## Notes on Apache-2.0 dependencies

Apache-2.0 requires preservation of NOTICE files when present in the upstream
distribution. Apache-2.0 dependencies in this list ship their NOTICE (where
applicable) inside the module/package itself; consumers redistributing this
software must preserve them. The license text and any NOTICE file are
available in the upstream repositories linked above.

## How to regenerate

```
make licenses
```

EOF
} >"$OUT_MD"

echo "  wrote $OUT_MD"
echo "Done."
