# imcodex

`imcodex` bridges a Lark, Feishu, or Telegram group chat to long-lived Codex
sessions running in `tmux`.

## Runtime Model

`v2.2` supports two Codex runtimes:

- `docker-codex`
  This is the default and recommended production mode.
- `host-codex`
  This is opt-in and intended for manual debugging only.

`docker-codex` no longer needs `runtime`, `runtime_config_dir`, or
`session_command` in YAML. `imcodex` now manages the Docker launcher
internally and auto-builds the local `stable` image when needed unless you
override it with a custom Docker image.

## Requirements

Always required:

- `tmux`
- a Lark / Feishu bot or a Telegram bot

Required for the default `docker-codex` runtime:

- `docker`
- the runtime user must be able to run `docker` without `sudo`

Required only for `--runtime host-codex`:

- `nodejs`
- `npm`
- `@openai/codex`
- `bubblewrap`

## Install On Ubuntu

Base install for the recommended Docker runtime:

```bash
sudo apt update
sudo apt install -y tmux docker.io bubblewrap
sudo usermod -aG docker "$USER"
```

Log out and log back in so the `docker` group change takes effect, then verify:

```bash
tmux -V
docker --version
docker run --rm hello-world
```

If you want the optional host runtime as well:

```bash
sudo apt install -y nodejs npm
sudo npm install -g @openai/codex
codex --version
codex login
```

## Build

```bash
go build -o imcodex .
```

## Configuration

If `-config` is omitted, `imcodex` looks for config files in this order:

1. `./imcodex.yaml`
2. `~/.imcodex.yaml`

See [config.example.yaml](config.example.yaml).

Key fields:

| Field | Meaning |
| --- | --- |
| `platform` | `lark` or `telegram` |
| `docker_image` | Optional custom image for `docker-codex`; when set, `imcodex` runs that image directly instead of rebuilding the managed local `stable` image |
| `interrupt_on_new_message` | If `true`, a new group message interrupts the current main-session run and keeps only the newest pending message |
| `groups[].group_id` | Group ID or Telegram chat ID |
| `groups[].cwd` | Working directory mapped to that group |
| `groups[].session_name` | Optional override for the tmux session name |
| `groups[].jobs[].name` | Job name shown in job result posts |
| `groups[].jobs[].schedule` | Standard 5-field cron expression |
| `groups[].jobs[].prompt_file` | Markdown prompt file for agent-driven jobs; relative paths resolve from the config file directory |
| `groups[].jobs[].command` | Shell command for deterministic CLI jobs; runs in `cwd` |
| `groups[].jobs[].artifacts_dir` | Optional base dir for per-run logs; relative paths resolve from `cwd` |
| `groups[].jobs[].summary_file` | Optional file whose content is posted on success; relative paths resolve from `cwd` |
| `groups[].jobs[].session_name` | Optional override for a `prompt_file` job session name |

Each job must set exactly one of `prompt_file` or `command`.

Path fields support:

- absolute paths
- `~/...`
- `$HOME/...`
- `${HOME}/...`

## Run

Recommended Docker runtime:

```bash
./imcodex -config /srv/imcodex/imcodex.yaml
```

Explicit Docker runtime:

```bash
./imcodex -config /srv/imcodex/imcodex.yaml --runtime docker-codex
```

Docker runtime with a non-default Codex config directory:

```bash
./imcodex -config /srv/imcodex/imcodex.yaml --codex-config-dir ~/.codex
```

Docker runtime with a custom prebuilt image:

```bash
./imcodex -config /srv/imcodex/imcodex.yaml
```

```yaml
docker_image: ghcr.io/acme/imcodex-go:1.24
```

Optional host runtime:

```bash
./imcodex -config /srv/imcodex/imcodex.yaml --runtime host-codex
```

## Docker Runtime Behavior

When `imcodex` runs in `docker-codex` mode:

- it uses the current `imcodex` binary itself as the tmux pane launcher
- if `docker_image` is unset, it ensures a local image tagged `imcodex-codex:stable` exists
- if that managed image is missing or stale, it rebuilds it automatically
- if `docker_image` is set, it runs that prebuilt image directly and skips managed-image rebuild checks
- it mounts only the configured group `cwd` into the container as `/workspace`
- it copies the host Codex config directory into container-local `/home/agent/.codex`
- it launches Codex inside the container with:

```bash
codex -a never -s danger-full-access --no-alt-screen -C /workspace
```

The pinned Docker Codex CLI version for `v2.2.1` is `0.118.0`.

If you want to prebuild the same image manually:

```bash
docker build \
  --build-arg CODEX_VERSION=0.118.0 \
  --build-arg IMCODEX_IMAGE_REVISION=2.2.1 \
  -t imcodex-codex:stable \
  -f tools/runtime/Dockerfile.codex .
```

Custom images should provide the same runtime contract:

- `bash`
- `codex`
- `gosu`
- writable `/home/agent`
- `/workspace` as the mounted workspace path

## Host Runtime Caveat

`host-codex` is kept for explicit manual use only.

It is not the recommended unattended mode because host-installed Codex may show
upgrade prompts that interrupt the session. If you need stable unattended
operation, use the default `docker-codex` runtime instead.

## Compatibility

`v2.2` removes these YAML fields:

- `runtime`
- `runtime_config_dir`
- `session_command`

If they still appear in config, `imcodex` now fails fast with a migration
error instead of silently mixing old and new runtime behavior.

Existing message routing, buffered Telegram output handling, scheduled jobs,
and `tmux` session reuse continue to work the same way.

## Message Delivery

`v2.2.1` tightens Telegram delivery behavior without changing the public config
surface:

- outbound send/edit/delete/chat-action calls now use bounded request timeouts
- detached reply chunks resume one at a time with per-chat spacing instead of
  draining the whole backlog at once
- editable reply sync no longer bypasses the normal edit throttle on every
  busy-to-idle transition
- watchdog retries no longer rewrite an editable body into plain detached body
  sends
- recovery after `429` no longer depends on a later unrelated inbound message

Current operator-facing behavior is documented in
[docs/telegram-output-buffering.md](docs/telegram-output-buffering.md).

The longer-term simplification plan remains in
[docs/message-delivery-redesign.md](docs/message-delivery-redesign.md).

## Runtime Docs

More detailed runtime notes:

- [docs/runtime-v2-docker-tmux.md](docs/runtime-v2-docker-tmux.md)
- [docs/runtime-v2-examples.md](docs/runtime-v2-examples.md)
- [docs/telegram-output-buffering.md](docs/telegram-output-buffering.md)
- [docs/message-delivery-redesign.md](docs/message-delivery-redesign.md)

## Example Startup Log

```text
imcodex 2.2.1 started: config=/srv/imcodex/imcodex.yaml platform=telegram runtime=docker-codex groups=1 jobs=1 base=https://api.telegram.org
```
