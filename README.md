# Pando

<picture>
  <source srcset="internal/relay/logo.webp" type="image/webp">
  <img src="internal/relay/logo.webp" alt="Pando" width="160">
</picture>

Private, encrypted terminal chat. End-to-end encrypted messages between people you trust, delivered through self-hosted relays, not stored on them. Messages for online recipients are delivered live and never retained by the relay; messages sent to offline recipients are queued for up to 24 hours or until they log in, whichever comes first.

## Install

### macOS / Linux — Homebrew

```bash
brew tap elpdev/tap
brew install elpdev/tap/pando
```

### Arch Linux — AUR

```bash
yay -S pando-bin
```

### Windows — Winget

```bash
winget install elpdev.pando
```

### Debian / Ubuntu — .deb

Download the `.deb` from the [latest release](https://github.com/elpdev/pando/releases/latest) and install it:

```bash
sudo dpkg -i pando_*.deb
```

### Fedora / RHEL — .rpm

```bash
sudo rpm -i pando_*.rpm
```

### Direct binary download

Pre-built archives for Linux, macOS, and Windows (amd64 and arm64) are available on the [latest release](https://github.com/elpdev/pando/releases/latest) page. Extract and place the binaries somewhere on your `$PATH`.

### From source

```bash
go install github.com/elpdev/pando/cmd/pando@latest
go install github.com/elpdev/pando/cmd/pando-relay@latest
```

## Quickstart

Start a relay:

```bash
pando-relay
```

Point a device at that relay and create a mailbox. Publishing the directory entry during init makes the mailbox reachable right away, and publishing again with `--discoverable` opts it into relay-backed discovery:

```bash
pando config set relay wss://pandorelay.network/ws
pando config set relay-token <relay-token>
pando config set mailbox alice

pando identity init --mailbox alice --publish-directory
pando contact publish-directory --mailbox alice --discoverable
```

On first use, Pando now asks for a passphrase for each mailbox and encrypts the local identity, contacts, and pending-enrollment files under `~/.pando/clients/<mailbox>/`. Later TUI and CLI sessions prompt for that passphrase before loading chat history.

If your relay does not require auth, skip `pando config set relay-token`.

Do the same on the other device with its own mailbox name:

```bash
pando config set mailbox bob

pando identity init --mailbox bob --publish-directory
pando contact publish-directory --mailbox bob --discoverable
```

Once both mailboxes are discoverable on the same relay, connect them with a contact request:

```bash
pando contact request --mailbox alice --contact bob
pando contact requests --mailbox bob
pando contact accept --mailbox bob --contact alice
```

Then start chatting:

```bash
pando --mailbox alice --to bob
```

Useful shortcuts:

- `pando contact discover` lists discoverable mailboxes on the relay.
- `pando contact lookup --mailbox alice --contact bob` imports a relay directory entry directly when you already trust that mailbox.
- `pando` starts the TUI using your configured defaults.

For invite-code exchange, QR sharing, device enrollment, and the lower-level contact flows, use the wiki.

### Storage location

By default, Pando stores all local data under `~/.pando`:

- client state: `~/.pando/clients/<mailbox>/`
- relay state: `~/.pando/relay/relay.db`

You can override the shared storage root with `-root-dir`, which is useful if you want to keep your chats and relay data on a removable drive:

```bash
pando identity init --mailbox alice --root-dir /media/usb/pando --publish-directory
pando --mailbox alice --to bob --root-dir /media/usb/pando
pando-relay --root-dir /media/usb/pando
```

More specific overrides still win when needed:

- `pando`: `-data-dir`
- `pando-relay`: `-store`

### Client passphrases

Per-mailbox client state under `~/.pando/clients/<mailbox>/` is protected with a passphrase:

- `identity.json`
- `contacts.json`
- `pending-enrollment.json`

That passphrase also gates local chat history decryption because history keys are derived from the unlocked identity.

Useful commands and environment variables:

```bash
# unlock non-interactively
export PANDO_PASSPHRASE='correct horse battery staple'

# change an existing mailbox passphrase
pando identity change-passphrase --mailbox alice

# change non-interactively
export PANDO_PASSPHRASE='current passphrase'
export PANDO_PASSPHRASE_NEW='new passphrase'
pando identity change-passphrase --mailbox alice
```

If stdin is not a terminal and `PANDO_PASSPHRASE` is unset, client commands fail fast instead of hanging on a hidden prompt.

### Relay configuration

Relay settings can be set via flags or environment variables — useful for container deployments:

```bash
export PANDO_RELAY_AUTH_TOKEN="your-shared-secret"
export PANDO_RELAY_ADDR=":8080"
pando-relay
```

Supported environment variables:

| Variable | Description |
|---|---|
| `PANDO_RELAY_ADDR` | Listen address (default `:8080`) |
| `PANDO_RELAY_AUTH_TOKEN` | Shared token required from all clients |
| `PANDO_RELAY_STORE_PATH` | Path to durable mailbox database |
| `PANDO_RELAY_QUEUE_TTL` | How long to hold messages for offline peers |
| `PANDO_RELAY_MAX_MESSAGE_BYTES` | Maximum envelope size |
| `PANDO_RELAY_MAX_QUEUED_MESSAGES` | Maximum queued messages per mailbox |
| `PANDO_RELAY_MAX_QUEUED_BYTES` | Maximum queued payload bytes per mailbox |
| `PANDO_RELAY_RATE_LIMIT_PER_MINUTE` | Per-connection rate limit |
| `PANDO_RELAY_ALLOWED_ORIGINS` | Comma-separated CORS allowed origins |
| `PANDO_RELAY_LANDING_PAGE` | Serve landing page at `/` (`true` or `false`)|

Flags take precedence over environment variables. Invalid values fail startup with an explicit error.

## Deploy

The relay is packaged as a Docker container:

```bash
docker build -t pando-relay .
docker run --rm -p 8080:8080 -v "$PWD/storage:/storage" pando-relay --addr :8080 --store /storage/relay.db
```

The image uses vendored Go dependencies and does not fetch modules at build time.

Every push to `main` publishes the relay image to GHCR:

- `ghcr.io/elpdev/pando-relay:latest`
- `ghcr.io/elpdev/pando-relay:main`
- `ghcr.io/elpdev/pando-relay:vX.Y.Z`
- `ghcr.io/elpdev/pando-relay:sha-<commit>`

And creates a GitHub Release with archives, `.deb`/`.rpm` packages, and a `checksums.txt` for all supported platforms. Version bumps follow conventional commits: `feat:` → minor, everything else → patch, `!` or `BREAKING CHANGE` → major.

The relay image is multi-arch: `linux/amd64` and `linux/arm64`.

Once running, the relay WebSocket endpoint is at `/ws` and the healthcheck at `/up`:

```bash
curl http://localhost:8080/up
# ws://localhost:8080/ws
# wss://relay.example.com/ws  (with TLS termination)
```

## CLI Reference

The `pando` binary handles both the TUI client and management subcommands. The commands most relevant to the relay discovery flow are:

| Command | Description |
|---|---|
| `pando` | Start the TUI chat client |
| `pando identity init` | Create a new identity for a mailbox (`--publish-directory` also publishes relay bootstrap state) |
| `pando identity change-passphrase` | Re-encrypt a mailbox's protected local state with a new passphrase |
| `pando contact publish-directory` | Publish the signed relay directory entry for a mailbox (`--discoverable` also lists it in relay discovery) |
| `pando contact discover` | List discoverable mailboxes published to the relay directory |
| `pando contact lookup` | Import a contact directly from the relay directory by mailbox |
| `pando contact request` | Send a relay-backed contact request to a discoverable mailbox |
| `pando contact requests` | List saved incoming and outgoing contact requests |
| `pando contact accept` | Accept a pending incoming contact request |
| `pando contact reject` | Reject a pending incoming contact request |
| `pando config show` | Show device-wide defaults |
| `pando config set relay <url>` | Set default relay URL |
| `pando config set relay-token <token>` | Set default relay auth token |
| `pando config set mailbox <mailbox>` | Set default mailbox |

## Docs

For full usage — invite-code exchange, QR sharing, contact management, device enrollment, relay hardening, and remote testing — see the **[Wiki](https://github.com/elpdev/pando/wiki)**.

## Current Limitations

- No automatic history sync to newly enrolled devices
- Delivery state is message-level only; no per-device breakdown or read receipts yet
