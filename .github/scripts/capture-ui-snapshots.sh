#!/usr/bin/env bash
set -euo pipefail

base_url="${BASE_URL:-http://127.0.0.1:8080}"
output_dir="${OUTPUT_DIR:-artifacts/ui-snapshots}"
playwright_cli_version="${PLAYWRIGHT_CLI_VERSION:-0.1.17}"
session="klipbord-ui-snapshots"

# playwright-cli keeps its daemon session outside the npm cache. Keep it in a
# temporary directory so local runs do not require access to a user-wide cache.
export PWTEST_DAEMON_SESSION_DIR="${PWTEST_DAEMON_SESSION_DIR:-${TMPDIR:-/tmp}/klipbord-playwright-daemon}"

mkdir -p "$output_dir"
output_dir="$(cd "$output_dir" && pwd)"

pwcli() {
  local config_args=()
  if [ "$1" = "open" ] && [ -n "${PLAYWRIGHT_CLI_CONFIG:-}" ]; then
    config_args=("--config=$PLAYWRIGHT_CLI_CONFIG")
  fi

  npx --yes --package="@playwright/cli@${playwright_cli_version}" \
    playwright-cli --session "$session" "$@" "${config_args[@]}"
}

cleanup() {
  pwcli close >/dev/null 2>&1 || true
  rm -rf "${fixtures_dir:-}"
}
trap cleanup EXIT

wait_for_server() {
  for _ in $(seq 1 30); do
    if curl --fail --silent --show-error "$base_url/health" >/dev/null; then
      return 0
    fi
    sleep 1
  done

  echo "Klipbord did not become ready at $base_url" >&2
  return 1
}

wait_for_server

fixtures_dir="${TMPDIR:-/tmp}/klipbord-ui-fixtures"
rm -rf "$fixtures_dir"
mkdir -p "$fixtures_dir"

cat >"$fixtures_dir/demo.md" <<'EOF'
# Launch notes

Klipbord keeps a team's files, snippets, and media ready to share.

* Drag files in
* Paste text
* Share one short link
EOF

printf '{"project":"Klipbord","status":"ready","features":["files","previews","sharing"]}\n' >"$fixtures_dir/demo.json"
printf 'name,team,status\nKlipbord,product,ready\nBrowser,quality,verified\n' >"$fixtures_dir/demo.csv"

# These tiny real media files give browser previews useful representative input
# without adding generated binary fixtures to the repository.
ffmpeg -hide_banner -loglevel error -f lavfi -i sine=frequency=440:duration=1 \
  -c:a libmp3lame "$fixtures_dir/demo.mp3"
ffmpeg -hide_banner -loglevel error -f lavfi -i color=c=0x2f6fed:s=640x360:d=1 \
  -f lavfi -i sine=frequency=440:duration=1 -shortest -c:v libx264 -pix_fmt yuv420p \
  -c:a aac "$fixtures_dir/demo.mp4"
printf '%s\n' '%PDF-1.4' '1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj' '2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj' '3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj' '4 0 obj<</Length 58>>stream' 'BT /F1 24 Tf 72 720 Td (Klipbord sample document) Tj ET' 'endstream endobj' '5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj' 'xref' '0 6' '0000000000 65535 f ' '0000000009 00000 n ' '0000000058 00000 n ' '0000000115 00000 n ' '0000000226 00000 n ' '0000000334 00000 n ' 'trailer<</Size 6/Root 1 0 R>>' 'startxref' '404' '%%EOF' >"$fixtures_dir/demo.pdf"

upload_file() {
  curl --fail --silent --show-error \
    --form "file=@$1" \
    --form 'ttl=7d' \
    "$base_url/api/upload" >/dev/null
}

text_item="$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --data '{"content":"Your files, notes, and media are ready to share.","name":"welcome.txt","ttl":"7d"}' \
  "$base_url/api/text")"
text_id="$(printf '%s' "$text_item" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')"

if [ -z "$text_id" ]; then
  echo "Could not read the seeded text item ID" >&2
  exit 1
fi

for image in internal/app/testdata/*.png; do
  upload_file "$image"
done

for fixture in "$fixtures_dir"/*; do
  upload_file "$fixture"
done

pwcli open "$base_url/clip"
pwcli resize 1440 1000
pwcli snapshot >"$output_dir/clipboard-desktop.snapshot.txt"
pwcli run-code "async page => { await page.locator('[data-item-id=\"$text_id\"]').waitFor(); }"
pwcli run-code "async page => { await page.locator('video').waitFor(); await page.waitForFunction(() => document.querySelector('.doc-preview[data-doc-type=\"markdown\"]')?.textContent !== 'Loading…'); }"
pwcli video-start "$output_dir/klipbord-demo.webm"
pwcli video-chapter "Share a file" --description="Upload an image from your browser" --duration=1500
pwcli run-code "async page => { await page.getByRole('button', { name: 'Browse files to upload' }).click(); }"
pwcli upload internal/app/testdata/sample_screenshot.png
pwcli run-code "async page => { await page.getByText('sample_screenshot.png', { exact: true }).last().waitFor(); await page.waitForTimeout(1000); }"
pwcli video-chapter "Preview everything" --description="Files, documents, audio, and video stay together" --duration=1500
pwcli run-code "async page => { await page.locator('video').scrollIntoViewIfNeeded(); await page.waitForTimeout(1500); }"
pwcli run-code "async page => { await page.screenshot({ path: '$output_dir/clipboard-desktop.png', fullPage: true }); }"

curl --fail --silent --show-error \
  --request PATCH \
  --header 'Content-Type: application/json' \
  --data '{"persistent":true}' \
  "$base_url/api/files/$text_id" >/dev/null

pwcli run-code "async page => { await page.getByRole('link', { name: 'Persistent', exact: true }).click(); }"
pwcli snapshot >"$output_dir/persistent-desktop.snapshot.txt"
pwcli run-code "async page => { await page.locator('#persistentGrid [data-item-id=\"$text_id\"]').waitFor(); }"
pwcli run-code "async page => { await page.locator('#persistentGrid [data-preview-id=\"$text_id\"] code').waitFor(); }"
pwcli video-chapter "Keep important items" --description="Pinned files have their own view" --duration=1500
pwcli run-code "async page => { await page.waitForTimeout(1500); }"
pwcli video-stop
pwcli run-code "async page => { await page.screenshot({ path: '$output_dir/persistent-desktop.png', fullPage: true }); }"

pwcli reload
pwcli run-code "async page => { if (page.url() !== '$base_url/persist') throw new Error('refresh did not keep /persist'); await page.locator('#tab-persistent.active').waitFor(); }"

pwcli run-code "async page => { await page.getByRole('link', { name: 'Clipboard', exact: true }).click(); }"
pwcli resize 390 844
pwcli snapshot >"$output_dir/clipboard-mobile.snapshot.txt"
pwcli run-code "async page => { await page.screenshot({ path: '$output_dir/clipboard-mobile.png', fullPage: true }); }"
