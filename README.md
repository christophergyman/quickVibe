# Claude Quick

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A terminal UI for managing tmux sessions across your devcontainers.

<!-- Screenshot placeholder - Add a GIF or image here -->
<!-- ![Claude Quick Demo](docs/demo.gif) -->

## Quick Start

```bash
# Install
go install github.com/christophergyman/claude-quick@latest

# Run
claude-quick
```

## Requirements

- Go 1.25+
- Docker
- [devcontainer CLI](https://github.com/devcontainers/cli) (`npm install -g @devcontainers/cli`)
- tmux inside your devcontainers

## Installation

**Go Install** (recommended)
```bash
go install github.com/christophergyman/claude-quick@latest
```

**Build from Source**
```bash
git clone https://github.com/christophergyman/claude-quick.git
cd claude-quick
go build -o claude-quick .
```

**Build Script** (builds + symlinks to ~/.local/bin)
```bash
./build.sh
```

## Usage

1. Launch `claude-quick` to see all discovered devcontainers
2. Select a container (starts it if stopped)
3. Choose or create a tmux session
4. Detach with `Ctrl+b d` to return to the dashboard

### Keybindings

| Key | Action |
|-----|--------|
| `j`/`k` or `↑`/`↓` | Navigate |
| `Enter` | Select / Connect |
| `x` | Stop container or session |
| `r` | Restart |
| `R` | Refresh status |
| `?` | Show config |
| `q` / `Esc` | Back / Quit |

## Configuration

Config file: `~/.config/claude-quick/config.yaml`

```yaml
search_paths:
  - ~/projects
  - ~/work
max_depth: 3
default_session_name: main
```

See [`config.example.yaml`](config.example.yaml) for all options.

## Nerd Font Support

For proper Unicode rendering in devcontainers (e.g., Claude CLI), add to your `devcontainer.json`:

```json
{
  "containerEnv": {
    "TERM": "xterm-256color",
    "LANG": "en_US.UTF-8"
  }
}
```

## License

MIT
