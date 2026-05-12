#!/usr/bin/env bash
# Generates CycloneDX SBOMs for the api binary, the worker binary, and the web
# package. Outputs go to /sbom/ at repo root (gitignored). Published as
# release artifacts by .github/workflows/compliance.yml.
#
# Usage: ./scripts/generate-sbom.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/sbom"
mkdir -p "$OUT"

echo "→ CycloneDX SBOM: api"
(
  cd "$ROOT/api"
  go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.10.0 app \
    -main ./cmd/api -json -output "$OUT/api.cdx.json" .
)

echo "→ CycloneDX SBOM: worker"
(
  cd "$ROOT/api"
  go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.10.0 app \
    -main ./cmd/worker -json -output "$OUT/worker.cdx.json" .
)

echo "→ CycloneDX SBOM: web"
(
  cd "$ROOT/web"
  npx --yes @cyclonedx/cyclonedx-npm@4.2.1 \
    --output-file "$OUT/web.cdx.json" \
    --output-format JSON
)

# Inter ships as loose woff2 files under web/src/assets/fonts/inter/, not as an
# npm package, so cyclonedx-npm doesn't see it. Splice in a `file` component so
# auditors get the OFL-1.1 attribution alongside the rest of the web tree.
echo "→ Adding Inter font to web SBOM"
INTER_TMP="$(mktemp)"
jq '.components += [{
  "type": "file",
  "name": "inter",
  "version": "4.1",
  "description": "Inter font (Rasmus Andersson), self-hosted woff2 assets under web/src/assets/fonts/inter/",
  "licenses": [{ "license": { "id": "OFL-1.1" } }],
  "externalReferences": [
    { "type": "website", "url": "https://rsms.me/inter/" },
    { "type": "vcs", "url": "https://github.com/rsms/inter" },
    { "type": "distribution", "url": "https://github.com/rsms/inter/releases/download/v4.1/Inter-4.1.zip" }
  ],
  "purl": "pkg:generic/inter@4.1"
}]' "$OUT/web.cdx.json" >"$INTER_TMP" && mv "$INTER_TMP" "$OUT/web.cdx.json"

echo "→ Verifying CycloneDX format"
for f in "$OUT/api.cdx.json" "$OUT/worker.cdx.json" "$OUT/web.cdx.json"; do
  if ! jq -e '.bomFormat == "CycloneDX"' "$f" >/dev/null; then
    echo "✗ $f is not a valid CycloneDX document" >&2
    exit 1
  fi
  echo "  $f ($(jq '.components | length' "$f") components)"
done

echo "Done. SBOMs in $OUT/"
