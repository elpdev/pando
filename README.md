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
pando identity init --mailbox alice
pando identity invite-code --mailbox alice --copy
pando contact add --mailbox alice --code '<bob-invite-code>'
pando contact list --mailbox alice
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

### Storage location

By default, Pando stores all local data under `~/.pando`:

- client state: `~/.pando/clients/<mailbox>/`
- relay state: `~/.pando/relay/relay.db`

You can override the shared storage root with `-root-dir`, which is useful if you want to keep your chats and relay data on a removable drive:

```bash
pando identity init --mailbox alice --root-dir /media/usb/pando
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
| `PANDO_RELAY_RATE_LIMIT_PER_MINUTE` | Per-connection rate limit |

Flags take precedence over environment variables. Invalid values fail startup with an explicit error.

## Deploy

The relay is packaged as a Docker container:

```bash
docker build -t pando-relay .
docker run --rm -p 8080:80 -v "$PWD/storage:/storage" pando-relay
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

## Docs

For full usage — contact management, device enrollment, relay hardening, and remote testing — see the **[Wiki](https://github.com/elpdev/pando/wiki)**.

## Current Limitations

- No automatic history sync to newly enrolled devices
- Delivery state is message-level only; no per-device breakdown or read receipts yet
