<div align="center">

<img src="https://raw.githubusercontent.com/jeeftor/klipbord/master/internal/webassets/static/icon.svg" width="120" alt="Klipbord logo" />

# Klipbord

**Self-hosted clipboard for AI agents and humans alike.**
Drop files, paste text, process images with vision LLMs — all through a slick web UI, REST API, and MCP server.

[![Release](https://img.shields.io/github/v/release/jeeftor/klipbord?style=flat-square&color=2f6fed)](https://github.com/jeeftor/klipbord/releases)
[![Docker](https://img.shields.io/badge/docker-ghcr.io%2Fjeeftor%2Fklipbord-2f6fed?style=flat-square&logo=docker&logoColor=white)](https://github.com/jeeftor/klipbord/pkgs/container/klipbord)
[![Go](https://img.shields.io/badge/go-1.26+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Tests](https://img.shields.io/github/actions/workflow/status/jeeftor/klipbord/ci.yml?style=flat-square&label=tests)](https://github.com/jeeftor/klipbord/actions)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Security](https://github.com/jeeftor/klipbord/actions/workflows/security.yml/badge.svg?branch=master)](https://github.com/jeeftor/klipbord/actions/workflows/security.yml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/github.com/jeeftor/klipbord/badge)](https://securityscorecards.dev/viewer/?uri=github.com/jeeftor/klipbord)

</div>

---

## Features

| | |
|--|--|
| **Web UI** | Ctrl+V image paste, drag-and-drop, text snippets, syntax highlighting, search |
| **Folder Archives** | Confirm a dropped folder to have the server create and upload one ZIP archive |
| **REST API** | List, upload, download, delete, pin files and text snippets |
| **MCP Server** | 20 tool calls for AI agents (Claude Code, Hermes, Devin, etc.) |
| **Vision Pre-processing** | Auto-OCR/describe uploaded images via any OpenAI-compatible vision LLM |
| **Multi-Prompt Analysis** | Analyze one image with multiple prompts — all results stored side-by-side |
| **Preset Comparison** | Run an image through all vision backends in parallel, rank with LLM judging |
| **Client-side Search** | Search filenames, vision OCR text, and paste content across both tabs |
| **Auto-expire** | Configurable TTL per item (1h, 1d, 7d, 30d, never) |
| **Persistent Pinning** | Mark items as persistent to exempt from expiry |
| **Smart MIME Detection** | Auto-detects file type from extension + content sniffing when client sends a generic type |
| **Short Links** | Shareable `/{id}/{filename}` URLs with auto-redirect, `?download=1`, and `?direct=1` |
| **Audio Waveform** | In-browser waveform player with play/pause, seek, and progress for MP3/WAV/OGG/FLAC |
| **OpenAPI 3.0** | Machine-readable spec at `/api/openapi.json` + Swagger UI |
| **Single Go binary** | No runtime dependencies; built-in race-enabled test and static-analysis checks |

---

## Quick Start

```bash
docker run -d \
  -p 127.0.0.1:8080:8080 \
  -v ./data:/data \
  -e BASE_URL=https://klipbord.example.com \
  ghcr.io/jeeftor/klipbord:latest
```

Then open `http://localhost:8080` — drop a file, paste an image, share a snippet.

When you put Klipbord behind Cloudflare Access, an Authentik proxy, or another
identity-aware reverse proxy, keep the container on loopback or a private Docker
network. Publishing the container port directly bypasses that access control.
The container runs as UID/GID `10001`; if you use an existing bind-mounted data
directory, make that directory writable by that UID/GID before upgrading.

### Native server binary

Klipbord also ships as a native server binary; Docker is not required. The web
UI is embedded and item data is stored under `DATA_DIR`. Install it with:

```bash
curl -fsSL https://github.com/jeeftor/klipbord/releases/latest/download/install.sh \
  | sh -s -- --component server

DATA_DIR=./data PORT=8080 kb-server
```

When `BASE_URL` is unset, `kb-server` selects an active non-loopback IPv4
address for generated LAN links. Set `BASE_URL` explicitly for reverse proxies,
TLS, or hosts with multiple network addresses. Install `ffmpeg` on the host if
you want media metadata probing; all core storage and sharing features need no
other runtime dependency.

---

## `kb-cli` Command-Line Client

`kb-cli` is the terminal companion for Klipbord. It reads standard input as a text
snippet and uploads file arguments directly:

```bash
cat a.txt | kb-cli
kb-cli screenshot.png report.pdf
kb-cli list
kb-cli pin ITEM_ID
kb-cli get ITEM_ID
kb-cli rm ITEM_ID
```

Uploads print their share URL to standard output, so they compose cleanly with
shell scripts. Use `--json` when a script needs item metadata.

### Installing `kb-cli`

**One-liner (macOS/Linux):**
```bash
curl -fsSL https://github.com/jeeftor/klipbord/releases/latest/download/install.sh | sh
```

The installer detects your OS/arch, downloads the matching binary, verifies
the SHA256 checksum, and optionally verifies the cosign signature if cosign
is installed. It installs to `~/.local/bin/kb-cli` (or `/usr/local/bin/kb-cli` as root).

Flags:
```bash
# Install a specific version
curl -fsSL https://github.com/jeeftor/klipbord/releases/latest/download/install.sh | sh -s -- --version v2.13.0

# Install to a custom directory
curl -fsSL https://github.com/jeeftor/klipbord/releases/latest/download/install.sh | sh -s -- --install-dir /opt/bin
```

**Manual install:** Download the matching `kb-cli` archive from the
[GitHub releases page](https://github.com/jeeftor/klipbord/releases), extract
it, and move the `kb-cli` binary to your PATH.

Once installed, run `kb-cli login`. Profile settings are stored in your normal
config directory while tokens and header values stay in your operating system
keychain.

```bash
# Cloudflare Access service token (recommended for a Cloudflare-protected URL)
export CF_ACCESS_CLIENT_ID='...'
export CF_ACCESS_CLIENT_SECRET='...'
kb-cli login --url https://klipbord.example.com --method cloudflare

# A direct OIDC-protected deployment with device authorization enabled
kb-cli login --url https://klipbord.example.com --method oidc \
  --issuer https://auth.example.com/application/o/klipbord/ \
  --client-id kb-cli
```

### Auto-discovery (Authentik forward_auth)

When Klipbord is behind Authentik forward_auth with CLI detection
configured in Caddy, `kb-cli login` auto-discovers the OIDC settings —
no `--method`, `--issuer`, or `--client-id` flags needed:

```bash
$ kb-cli login
Klipbord server URL [https://klipbord.example.com]:  (press enter)
Detected auth method: oidc
Open https://auth.example.com/device?code=810392209 and complete login using code 810392209
Waiting for authorization...
Logged in. Profile "default" is ready.
```

The CLI sends `User-Agent: klipbord-cli/<version>` on all requests.
Caddy detects this and, when forward_auth rejects an unauthenticated
request, returns a `401` with `X-OIDC-Issuer`, `X-OIDC-Client-ID`, and
`X-OIDC-Scopes` headers instead of a `302` redirect. The CLI reads
these headers to auto-configure the device code flow.

See `caddy/flows.md` (Pattern F) in the homelab repo for the full
Caddy and Authentik configuration details.

### Status and profiles

```bash
$ kb-cli status
Profile: default
Server: https://klipbord.example.com
Method: oidc
Status: logged in
```

Other supported methods are `bearer`, `headers`, and `none` for loopback-only
development. `headers` accepts repeated header names and securely prompts for
their values; `oidc` uses OpenID Connect discovery and refresh tokens.
Use `kb-cli profile list` and `kb-cli profile use NAME` when you have more than one
Klipbord connection.

### Authentik app-password fallback

When the Klipbord Authentik proxy provider has HTTP Basic authentication
enabled, create an **App Password** from your Authentik user credentials page.
It is distinct from an Authentik API token. Then log in without exposing the
password in your shell history:

```bash
kb-cli login --url https://klipbord.example.com \
  --method authentik-app-password \
  --username your-authentik-username
```

`kb-cli` prompts for the app password and stores the resulting Basic authorization
header in your operating system keychain. For unattended use, set
`AUTHENTIK_USERNAME` and `AUTHENTIK_APP_PASSWORD` in the calling environment.

Use `--log-level debug` with `kb-cli login` to diagnose discovery or OIDC setup.
It prints request URLs, response statuses, and OAuth error codes while redacting
device codes, access tokens, refresh tokens, and configured headers.

### Version management

```bash
$ kb-cli version
kb-cli version v2.14.0

$ kb-cli update --check
An update is available: v2.14.0 (current: v2.13.0). Run 'kb-cli update' to upgrade.

$ kb-cli update
Updating kb-cli v2.13.0 → v2.14.0...
== Installed kb-cli to /home/user/.local/bin/kb-cli
```

`kb-cli` also checks for updates automatically once per day when uploading
files. If a newer version is available, it prints a notice to stderr
(never blocks or fails the upload).

---

## Configuration

### Environment Variables

| Env Var | Default | Description |
|---------|---------|-------------|
| `PORT` | `8080` | HTTP port |
| `DATA_DIR` | `/data` | Storage directory |
| `BASE_URL` | Active LAN IPv4 address, or `http://localhost:8080` | Public URL for generating links; set explicitly for proxies/TLS/multi-homed hosts |
| `MAX_UPLOAD_MB` | `2048` | Max upload size in MB |
| `VISION_ENABLED` | `true` | Enable automatic image analysis on upload |
| `VISION_REQUEST_TIMEOUT` | `2m` | Maximum time for each matrix inference request |
| `VISION_UNLOAD_TIMEOUT` | `2m` | Maximum time to wait for unload and observed memory release |
| `VISION_ENDPOINT` | *(see presets)* | OpenAI-compatible vision LLM endpoint (overrides UI config) |
| `VISION_MODEL` | *(see presets)* | Vision model name to use (overrides UI config) |

### Vision LLM

Configure via the **Config tab** in the UI or via environment variables.

**Env vars always win** — if `VISION_ENDPOINT` or `VISION_MODEL` are set they override the UI. A locked "env" preset appears in the UI to indicate this.

Three presets ship on first boot:

| Preset | Endpoint | Model |
|--------|----------|-------|
| `lemonade` | `http://localhost:13305/v1/chat/completions` | `Qwen3-VL-4B-Instruct-GGUF` |
| `ollama` | `http://localhost:11434/v1/chat/completions` | `llama3.2-vision` |
| `openai` | `https://api.openai.com/v1/chat/completions` | `gpt-4o-mini` |

To disable vision entirely: `VISION_ENABLED=false`

---

## Vision Pre-Processing

When an image is uploaded, Klipbord automatically sends it to the configured vision LLM. The result (extracted text + description) is stored alongside the image and surfaced via MCP tools and the REST API.

### Built-In Prompts

| Prompt | Use Case |
|--------|----------|
| `default` | General-purpose analysis — OCR + description |
| `terminal` | Terminal screenshots — commands, output, errors |
| `code` | Code screenshots — preserves indentation, detects language |
| `document` | Documents/receipts — structured layout extraction |
| `diagram` | Diagrams/charts — structure, connections, flow |

### Custom Prompts

```bash
# Create
curl -X POST -H 'Content-Type: application/json' \
  -d '{"name":"ui_mockup","description":"UI mockup analysis","prompt":"Analyze this UI mockup..."}' \
  /api/prompts

# Update
curl -X PUT -H 'Content-Type: application/json' \
  -d '{"description":"Updated"}' /api/prompts/ui_mockup

# Delete
curl -X DELETE /api/prompts/ui_mockup
```

### Multi-Prompt Analysis

```bash
curl -X POST /api/analyze/{id}?prompt=terminal
curl -X POST /api/analyze/{id}?prompt=code
curl /api/files/{id}   # both results returned in analyses field
```

---

## REST API

<details>
<summary><strong>Items</strong></summary>

```bash
curl /api/files                                          # list all
curl -F 'file=@screenshot.png' -F 'ttl=7d' /api/upload  # upload file (MIME auto-detected)
curl /api/files/{id} -o file.png                         # download
curl -X POST -H 'Content-Type: application/json' \
  -d '{"content":"hello","name":"note.txt","ttl":"7d"}' /api/text   # create text snippet
curl /api/text/{id}                                      # get raw text
curl -X DELETE /api/files/{id}                           # delete
curl -X PATCH -H 'Content-Type: application/json' \
  -d '{"persistent":true}' /api/files/{id}              # pin/unpin
curl -X PATCH -H 'Content-Type: application/json' \
  -d '{"mime_type":"image/gif"}' /api/files/{id}        # fix MIME type
```

</details>

<details>
<summary><strong>Vision</strong></summary>

```bash
curl -X POST /api/analyze/{id}?prompt=terminal           # trigger analysis
curl -X POST /api/vision/test                            # test with built-in sample
curl -X POST -H 'Content-Type: application/json' \
  -d '{"image_type":"code"}' /api/vision/test            # test specific image type
curl -X POST -H 'Content-Type: application/json' \
  -d '{"image_type":"terminal"}' /api/vision/compare     # compare all presets
curl -X POST -H 'Content-Type: application/json' \
  -d '{"item_id":"abc123"}' /api/vision/compare          # compare using uploaded image
curl -X POST -H 'Content-Type: application/json' \
  -d '{"image_type":"terminal"}' /api/vision/compare-prompts  # compare all prompts
```

</details>

<details>
<summary><strong>Vision Config</strong></summary>

```bash
curl /api/config/vision                                  # get config
curl -X POST -H 'Content-Type: application/json' \
  -d '{"preset":"ollama"}' /api/config/vision/active     # set active preset
curl -X POST -H 'Content-Type: application/json' \
  -d '{"enabled":false}' /api/config/vision/enabled      # toggle vision
curl -X POST -H 'Content-Type: application/json' \
  -d '{"name":"my-llm","endpoint":"http://localhost:8080/v1/chat/completions","model":"my-model"}' \
  /api/config/vision/presets                             # create preset
curl -X DELETE /api/config/vision/presets/my-llm        # delete preset
curl -X POST -H 'Content-Type: application/json' \
  -d '{"preset":"lemonade"}' /api/config/vision/test     # test connection
```

</details>

<details>
<summary><strong>Prompts</strong></summary>

```bash
curl /api/prompts                                        # list all
curl /api/prompts/{name}                                 # get one
curl -X POST -H 'Content-Type: application/json' \
  -d '{"name":"x","description":"y","prompt":"z"}' /api/prompts   # create
curl -X PUT -H 'Content-Type: application/json' \
  -d '{"prompt":"new text"}' /api/prompts/{name}         # update
curl -X DELETE /api/prompts/{name}                       # delete (custom only)
```

</details>

<details>
<summary><strong>Health</strong></summary>

```bash
curl /api/health      # → {"status":"ok"}
curl /api/version     # → {"version":"vX.Y.Z"}
curl /api/openapi.json
```

</details>

---

## MCP Server

```json
{ "mcpServers": { "kb": { "url": "https://klipbord.example.com/mcp" } } }
```

<details>
<summary><strong>All 20 tools</strong></summary>

| Tool | Description |
|------|-------------|
| `list_files` | List all items with metadata |
| `get_file` | Get file content (base64) or text content |
| `get_file_url` | Get public URL for an item |
| `upload_file` | Upload a file (base64 content + filename) |
| `create_text` | Create a text snippet |
| `get_text` | Get raw text snippet content |
| `delete_file` | Delete an item |
| `persist_file` | Pin or unpin an item |
| `describe_image` | Get vision analysis for an image |
| `analyze_image` | Trigger/re-trigger vision analysis |
| `inspect_image` | Ask a focused visual question and return structured visible evidence |
| `list_prompts` | List all available vision prompts |
| `create_prompt` | Create a new vision prompt template |
| `update_prompt` | Update an existing prompt |
| `delete_prompt` | Delete a custom prompt |
| `list_vision_presets` | List all configured vision LLM presets |
| `set_vision_preset` | Switch the active vision LLM preset |
| `test_vision_preset` | Test connectivity to a preset |
| `test_vision` | Run the full vision pipeline on a sample image |
| `compare_vision` | Run image through ALL presets, rank results |
| `compare_prompts` | Run image through ALL prompts, rank results |

</details>

<details>
<summary><strong>Example tool calls</strong></summary>

```jsonc
// Describe an image
{"name": "describe_image", "arguments": {"id": "abc123"}}

// With a specific prompt
{"name": "describe_image", "arguments": {"id": "abc123", "prompt": "terminal"}}

// Trigger re-analysis
{"name": "analyze_image", "arguments": {"id": "abc123", "prompt": "code"}}

// Ask a question with UI evidence tailored for a text-only agent
{"name": "inspect_image", "arguments": {"id": "abc123", "mode": "ui", "question": "Which tab is selected, and is the clipboard still loading?"}}

// Compare all presets on a sample image
{"name": "compare_vision", "arguments": {"image_type": "terminal"}}

// Compare all prompts
{"name": "compare_prompts", "arguments": {"image_type": "terminal"}}

// Switch active preset
{"name": "set_vision_preset", "arguments": {"preset": "ollama"}}
```

</details>

---

## Direct Links

```
https://klipbord.example.com/{id}/{filename}     # canonical share link (filename visible in URL)
https://klipbord.example.com/{id}                # redirects to /{id}/{filename}
https://klipbord.example.com/{id}?direct=1       # serves inline, no redirect (curl -O friendly)
https://klipbord.example.com/{id}?download=1     # redirects to named URL, forces download
https://klipbord.example.com/link/{id}           # legacy form (still works)
```

---

## Web UI Routes

Each UI section has a stable URL, so it remains selected after a refresh and can be bookmarked.

| Route | UI section |
|-------|------------|
| `/clip` | Clipboard items and upload controls |
| `/persist` | Persistent items |
| `/config` | Vision configuration |
| `/mcp-web` | MCP setup and tool reference |
| `/rest-web` | REST API reference |

`/` redirects to `/clip`. The browser UI routes are intentionally separate from the machine interfaces: REST remains under `/api/...`, MCP remains at `/mcp`, and direct item links use the short form `/{id}` (with `/link/{id}` as a legacy alias).

---

## Storage Layout

| Path | Content |
|------|---------|
| `{DATA_DIR}/files/` | Uploaded files |
| `{DATA_DIR}/text/` | Text snippets |
| `{DATA_DIR}/chunks/` | Chunked upload temp files |
| `{DATA_DIR}/metadata.json` | Item metadata (IDs, names, MIME, TTL, analyses) |
| `{DATA_DIR}/prompts.json` | Custom vision prompts |
| `{DATA_DIR}/vision_config.json` | Vision LLM presets and active selection |

---

## Changelog

### v2.12.1

- **Reliable CLI releases**: The binary publishing job now checks out the tagged source before creating the GitHub release and uploading its verified archives.

### v2.12.0

- **`kb` command-line client**: Upload files or piped text, manage items, and authenticate through Cloudflare Access service tokens, bearer tokens, custom headers, or OIDC device login. Release assets now include macOS, Linux, and Windows `kb` binaries.
- **Container-backed visual demo**: GitHub Actions runs the Docker image, seeds mixed media, and uploads screenshots plus a short browser-recorded WebM demo as artifacts.
- **Browse files control**: The clipboard input now exposes a dedicated multi-file upload button.

### v2.9.0

- **Media metadata via ffprobe**: Audio and video uploads are probed with `ffprobe` (included in Docker image) to extract codec, duration, bitrate, sample rate, channels, and resolution. Metadata is stored in the item record and displayed immediately on page load — no need to play the file first.
- **Waveform fix**: Audio waveforms now render on page load using `OfflineAudioContext` (no user gesture required). Previously the waveform only appeared after clicking play due to browser autoplay policy suspending regular `AudioContext`.
- **Video info overlay**: Video cards show resolution, duration, codec, and bitrate as an overlay on the video player.

### v2.8.0

- **Telegram notifications**: CI sends release announcements and CI failure alerts to a Telegram channel via bot. Requires `TELEGRAM_TOKEN` and `TELEGRAM_TO` repo secrets.
- **Canonical audio MIME types**: Registers `.mp3`, `.wav`, `.ogg`, `.flac`, `.m4a`, `.aac`, `.opus` at init time via `mime.AddExtensionType` so detection is consistent across Linux and macOS.

### v2.7.0

- **Audio waveform player**: Audio files (MP3, WAV, OGG, FLAC, M4A) now show a canvas waveform with play/pause, click-to-seek, and progress overlay. Peaks are computed client-side via the Web Audio API — no backend dependencies. Waveforms lazy-decode when scrolled into view.
- **Single-line toolbar**: Header and tab bar merged into one row — logo+version (left), tabs (center), help+TTL (right). More compact, less vertical space wasted.

### v2.6.0

- **Short links**: Shareable URLs are now `/{id}/{filename}` instead of `/link/{id}`. Bare `/{id}` auto-redirects to the named form so download tools and AI agents see the correct filename. `/link/{id}` still works as a legacy alias.
- **MIME auto-detection**: Uploads with a generic `Content-Type` (e.g. `application/octet-stream` from curl) now detect the real type from the filename extension first, then content sniffing. A `.gif` uploaded via curl is correctly stored as `image/gif`.
- **Download button**: File and image cards now have a dedicated download button that forces `Content-Disposition: attachment`.
- **MIME type editing**: `PATCH /api/files/{id}` accepts a `mime_type` field. An in-app tag icon on each file card lets you fix a wrong MIME type manually.
- **HEAD support**: `HEAD /{id}` returns headers (filename, content-type, size) without the body — useful for AI tools to cheaply inspect an item.
- **`?direct=1`**: Skips the filename redirect for `curl -O` compatibility without `-L`.

### v2.5.9

- Vision evidence inspection (`inspect_image` MCP tool)
- Live log drawer
- Clipboard loading state fixes

---

## Development

```bash
make build   # go build -o kb-server .
make run     # go run .
make test    # go test ./...
```

Tests use a mock vision server — no external LLM required. CI runs tests before building the Docker image; the `build` job is gated on `test`.

### Telegram Notifications

CI sends notifications to Telegram on release publishes and CI failures. To enable, set two repo secrets:

```bash
gh secret set TELEGRAM_TOKEN --repo $(gh repo view --json nameWithOwner -q .nameWithOwner)
gh secret set TELEGRAM_TO --repo $(gh repo view --json nameWithOwner -q .nameWithOwner)
```

- `TELEGRAM_TOKEN` — bot token from [@BotFather](https://t.me/BotFather)
- `TELEGRAM_TO` — channel ID (`@yourchannel` or `-1001234567890` for private channels)

---

<div align="center">
MIT License &nbsp;·&nbsp; <a href="https://github.com/jeeftor/klipbord">github.com/jeeftor/klipbord</a>
</div>
