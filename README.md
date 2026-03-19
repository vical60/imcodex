# imcodex

A lightweight bridge between Lark group chats and long-running Codex CLI sessions inside `tmux`.

| Item | Details |
|---|---|
| Model | `1 process = 1 Lark long connection = many group_id -> cwd -> tmux -> codex routes` |
| Input path | Text sent in a Lark group is forwarded to the matching Codex session |
| Output path | `imcodex` captures the `tmux` pane, computes a diff, and posts new output back to the group |
| Session model | Each configured group gets its own dedicated `tmux` session and Codex console |
| Network model | No public webhook, no inbound port, no callback URL |

## Why This Design

| Option | Result |
|---|---|
| Direct PTY driving of the Codex TUI | Fragile; terminal detection and screen handling are easy to break |
| `codex app-server` | More structured, but not the native interactive CLI experience |
| `Lark long connection + tmux + codex --no-alt-screen` | Simple, robust, observable, and closest to “a chat room as a Codex terminal” |

```text
Lark long connection
  -> imcodex
  -> group_id -> cwd
  -> tmux
  -> codex --no-alt-screen -C <cwd>
  -> capture-pane diff
  -> Lark group messages
```

## Repository Layout

```text
imcodex/
  LICENSE
  main.go
  config.go
  config.example.yaml
  Makefile
  README.md
  internal/gateway/
  internal/lark/
  internal/tmuxctl/
```

## Requirements

| Requirement | Notes |
|---|---|
| Go 1.24+ | Required to build from source |
| `tmux` in `PATH` | Required at runtime |
| `codex` in `PATH` | Required at runtime |
| Lark app credentials | `app_id` and `app_secret` |
| `upx` | Optional for release compression; required for `make linux` |

## Install

### macOS

Install runtime dependencies:

```bash
brew install tmux
npm install -g @openai/codex
codex login
```

Optional build dependency:

```bash
brew install upx
```

If you already use the Codex desktop app and want to use its bundled CLI instead of the npm package:

```bash
echo 'export PATH="/Applications/Codex.app/Contents/Resources:$PATH"' >> ~/.zshrc
source ~/.zshrc
codex --version
```

### Ubuntu 24.04

Install runtime dependencies:

```bash
sudo apt update
sudo apt install -y tmux

# Install Node.js + npm if they are not already available, then:
sudo npm install -g @openai/codex
codex login
```

Optional build dependency:

```bash
# Install UPX from your distro packages or an upstream release if you want compressed builds.
```

### Verify Your Toolchain

```bash
go version
tmux -V
codex --version
```

## Quick Start

```bash
cp config.example.yaml config.yaml
$EDITOR config.yaml
make
./build/imcodex-$(go env GOOS)-$(go env GOARCH) -config config.yaml
```

## Build

| Command | Output |
|---|---|
| `make` | Builds the current platform binary as `./build/imcodex-<goos>-<goarch>` |
| `make linux` | Builds a compressed Linux release as `./build/imcodex-linux-amd64` |
| `make test` | Runs `go test ./...` and `go test -race ./...` |

Compression notes:

| Target | Behavior |
|---|---|
| macOS local binary | UPX is intentionally skipped; packed binaries are killed by macOS at launch |
| Linux release binary | UPX compression is enabled in `make linux` |

## Lark Setup

1. Create or open your Lark app.
2. Enable bot capability.
3. Subscribe to the message receive event (`im.message.receive_v1`, shown in some consoles as “Receive message v2.0”).
4. Add the bot to every target group.
5. Copy the group ID directly from the group settings in the Lark client.

Group ID lookup:

| Step | Action |
|---|---|
| 1 | Open the target group in Lark |
| 2 | Open Group Settings / Chat Settings |
| 3 | Copy the Group ID or Chat ID directly from the UI |
| 4 | Put that value into `groups[].group_id` |

No API Explorer step is required.

## Configuration

The default config path is `./imcodex.yaml`. You can override it with `-config /path/to/imcodex.yaml`.

| Key | Description |
|---|---|
| `lark_app_id` / `LARK_APP_ID` | Lark app ID |
| `lark_app_secret` / `LARK_APP_SECRET` | Lark app secret |
| `lark_base_url` / `LARK_BASE_URL` | `https://open.larksuite.com` for Lark, `https://open.feishu.cn` for Feishu China |
| `groups[].group_id` | Target Lark group ID |
| `groups[].cwd` | Working directory for the Codex session mapped to that group |

Start from the example file:

```bash
cp config.example.yaml config.yaml
$EDITOR config.yaml
```

Minimal example:

```yaml
lark_app_id: cli_xxx
lark_app_secret: your_app_secret
lark_base_url: https://open.larksuite.com
groups:
  - group_id: oc_xxx
    cwd: /srv/my-project
  - group_id: oc_yyy
    cwd: /srv/another-project
```

You can also keep secrets in the environment:

```bash
export LARK_APP_ID=cli_xxx
export LARK_APP_SECRET=your_app_secret
export LARK_BASE_URL=https://open.larksuite.com
```

Do not commit a real `config.yaml` with production secrets.

## Run

From source:

```bash
go run . -config config.yaml
```

From a built binary:

```bash
./build/imcodex-$(go env GOOS)-$(go env GOARCH) -config config.yaml
```

## Runtime Behavior

| Scenario | Behavior |
|---|---|
| Text posted in a configured group | Forwarded to the matching Codex session |
| Slash commands such as `/new`, `/compact`, `/status` | Passed through unchanged to Codex |
| Multiple users in the same group | Serialized through a per-group queue |
| Multiple groups at once | Each group runs independently in its own `tmux` session |
| Codex produces new output | `imcodex` captures the pane and pushes incremental updates back to Lark |
| `imcodex` restarts | Reuses existing `tmux` sessions |
| A `tmux` session is missing | Recreated automatically on the next message |

## Observe a Session

Each group session gets a generated `tmux` session name in the form:

```text
imcodex-<cwd-base>-<group-id>
```

Useful commands:

```bash
tmux ls
tmux attach -r -t <session>
tmux capture-pane -pJ -S -200 -t <session>:0.0
```

## Limits

| Item | Notes |
|---|---|
| Streaming granularity | Near-real-time line-level updates, not token-level streaming |
| Interactive menu commands such as `/model` | Best handled directly inside `tmux` |
| Status-bar-heavy terminal output | More reliable in `tmux` than in Lark |
| Manual takeover | Read-only attach is recommended; avoid typing into the same session at the same time as the bridge |

## Development

```bash
make
make linux
make test
```

## License

MIT. See [LICENSE](LICENSE).

## References

- [Codex CLI overview](https://openai.com/codex/)
- [Codex CLI getting started](https://help.openai.com/en/articles/11096431-openai-codex-cli-getting-started)
- [Codex CLI sign-in with ChatGPT](https://help.openai.com/en/articles/11381614-codex-cli-and-sign-in-with-chatgpt)
- [Lark Developer](https://open.larksuite.com/?lang=en-US)
- [Receive message event: `im.message.receive_v1`](https://open.feishu.cn/document/server-docs/im-v1/message/events/receive)
- [Create message API: `POST /open-apis/im/v1/messages`](https://open.feishu.cn/document/server-docs/im-v1/message/create)
