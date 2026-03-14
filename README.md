# fldocs

**fldocs** is a CLI tool that lets you search and read Flutter documentation offline — no browser needed. It bundles the full Flutter docs so everything works instantly after install.

## Key Features

- Search the entire Flutter docs instantly from your terminal
- Read full documentation pages without opening a browser
- Works completely offline — docs are bundled inside the binary
- Integrates with Claude Code and other AI assistants as an MCP server
- Single binary — no runtime dependencies required

## Requirements

- macOS, Linux, or Windows

## Installation

### Homebrew
```bash
brew install AndroidPoet/tap/fldocs
```

### Download binary
Download the latest release from [GitHub Releases](https://github.com/AndroidPoet/fldocs-flutter/releases).

## Usage

**Search**
```bash
fldocs search "animation"
fldocs search "navigation"
fldocs search "state management"
```

**Read a page**
```bash
fldocs get ui/widgets/basics
fldocs get get-started/install
```

**Browse all pages**
```bash
fldocs ls
```

**Stats**
```bash
fldocs stats
```

## Claude Code Integration

Add to your Claude Code MCP settings:

```json
{
  "mcpServers": {
    "fldocs": {
      "command": "/usr/local/bin/fldocs",
      "args": ["mcp"]
    }
  }
}
```

Claude can now search and read Flutter docs directly inside your conversations.
