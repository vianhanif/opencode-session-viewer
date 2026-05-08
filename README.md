# opencode-session-viewer

CLI tool to inspect [opencode](https://opencode.ai) session metadata from the local SQLite database.

## Quick start

```bash
# link the script
ln -sf "$(pwd)/opencode-session.sh" ~/.local/bin/opencode-session

# list recent sessions
opencode-session

# show session details
opencode-session ses_xxx

# list more/less sessions
opencode-session --limit 50
opencode-session -n 5
```

See [docs/opencode-session-inspector.md](docs/opencode-session-inspector.md) for full documentation.
