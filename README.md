# Pando

<picture>
  <source srcset="internal/relay/logo.webp" type="image/webp">
  <img src="internal/relay/logo.webp" alt="Pando" width="160">
</picture>

Private, encrypted terminal chat. End-to-end encrypted messages between people you trust, delivered through a network of self-hosted relays.

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

## Run

Start the relay:

```bash
pando-relay
```

Initialize an identity and exchange contacts:

```bash
pando identity init --mailbox alice --publish-directory
pando identity invite-code --mailbox alice --copy
pando contact add --mailbox alice --code '<bob-invite-code>'
pando contact list --mailbox alice
```

If you are connecting to a relay for the first time, publish your signed relay directory entry before starting the chat client. The easiest way is to do it during init with `pando identity init --publish-directory`. You can also publish later with `pando contact publish-directory --mailbox <mailbox>`.

### Fastest relay setup for a new mailbox

Point a device at your relay, create the mailbox, publish its signed relay directory entry, and opt into relay-backed discovery:

```bash
pando config set relay wss://pandorelay.network/ws
pando config set relay-token <relay-token>
pando config set mailbox cousin

pando identity init --mailbox cousin --publish-directory
pando contact publish-directory --mailbox cousin --discoverable
```

If your relay does not require auth, skip `pando config set relay-token`.

### Fastest way to connect on the same relay

If both people published with `--discoverable`, the requester can send a relay-backed contact request without manually exchanging invite codes:

```bash
pando contact request --mailbox alice --contact bob
pando contact requests --mailbox bob
pando contact accept --mailbox bob --contact alice
```

`pando contact request` looks up the recipient in the relay directory, so the recipient must already have published a discoverable directory entry.

If you already trust the relay directory entry and want to add a contact immediately by mailbox, you can skip the request flow:

```bash
pando contact lookup --mailbox alice --contact bob
```

`pando contact add` now verifies the imported contact automatically. If you want to import without marking the contact trusted yet, use `pando contact import` and then run `pando contact verify` later.

### Fastest way to connect

The easiest invite flows are:

1. Copy and paste the raw invite code:

```bash
pando identity invite-code --mailbox leo --raw
pando contact add --mailbox alice --paste
```

2. Use the clipboard directly:

```bash
pando identity invite-code --mailbox leo --copy
pando contact add --mailbox alice --from-clipboard
```

3. Pipe the invite code between local shells:

```bash
pando identity invite-code --mailbox leo --raw | pando contact add --mailbox alice --stdin
```

4. Share or scan a QR code in the terminal:

```bash
pando identity invite-code --mailbox leo --qr
pando contact add --mailbox alice --qr-image /path/to/invite-qr.png
```

`pando contact add --paste` also accepts the full multiline output from `pando identity invite-code` and extracts the `invite-code:` value automatically.

Start the client:

```bash
pando
```

Start a chat with a specific contact:

```bash
pando --mailbox alice --to bob
```

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

The `pando` binary handles both the TUI client and management subcommands:

| Command | Description |
|---|---|
| `pando` | Start the TUI chat client |
| `pando identity init` | Create a new identity for a mailbox (`--publish-directory` also publishes relay bootstrap state) |
| `pando identity show` | Display identity details (fingerprint, devices) |
| `pando identity invite-code` | Generate an invite code (`--raw`, `--copy`, `--qr`) |
| `pando identity export-invite` | Export invite bundle to a JSON file |
| `pando contact add` | Add and verify a contact (`--code`, `--paste`, `--from-clipboard`, `--stdin`, `--qr-image`) |
| `pando contact import` | Import a contact without auto-verifying |
| `pando contact discover` | List discoverable mailboxes published to the relay directory |
| `pando contact request` | Send a relay-backed contact request to a discoverable mailbox |
| `pando contact requests` | List saved incoming and outgoing contact requests |
| `pando contact accept` | Accept a pending incoming contact request |
| `pando contact reject` | Reject a pending incoming contact request |
| `pando contact invite start` | Start a live relay rendezvous and print a short invite code |
| `pando contact invite accept` | Join a live relay rendezvous using a short invite code |
| `pando contact list` | List all contacts |
| `pando contact show` | Show contact details |
| `pando contact verify` | Mark a contact as verified |
| `pando contact lookup` | Import a contact directly from the relay directory by mailbox |
| `pando contact publish-directory` | Publish the signed relay directory entry for a mailbox (`--discoverable` also lists it in relay discovery) |
| `pando device list` | List enrolled devices |
| `pando device revoke` | Revoke a device |
| `pando device enroll create` | Create an enrollment request for a new device |
| `pando device enroll approve` | Approve an enrollment request |
| `pando device enroll complete` | Complete enrollment on the new device |
| `pando config show` | Show device-wide defaults |
| `pando config set relay <url>` | Set default relay URL |
| `pando config set relay-token <token>` | Set default relay auth token |
| `pando config set mailbox <mailbox>` | Set default mailbox |
| `pando eject` | Permanently delete local data for a mailbox |

## Docs

For full usage — contact management, device enrollment, relay hardening, and remote testing — see the **[Wiki](https://github.com/elpdev/pando/wiki)**.

## Current Limitations

- No automatic history sync to newly enrolled devices
- Delivery state is message-level only; no per-device breakdown or read receipts yet
