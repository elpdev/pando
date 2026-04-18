# Pando

<picture>
  <source srcset="internal/relay/logo.webp" type="image/webp">
  <img src="internal/relay/logo.webp" alt="Pando" width="160">
</picture>

Private, encrypted terminal chat. End-to-end encrypted messages between people you trust, delivered through a network of self-hosted relays.

## Install

Install all three binaries to your `$GOPATH/bin`:

```bash
go install github.com/elpdev/pando/cmd/pando@latest
go install github.com/elpdev/pando/cmd/pando-relay@latest
go install github.com/elpdev/pando/cmd/pandoctl@latest
```

Or download pre-built binaries for Linux, macOS, and Windows from the [latest release](https://github.com/elpdev/pando/releases/latest).

## Run

Start the relay:

```bash
pando-relay
```

Initialize an identity and exchange contacts:

```bash
pandoctl init --mailbox alice
pandoctl invite-code --mailbox alice --copy
pandoctl add-contact --mailbox alice --code '<bob-invite-code>'
pandoctl list-contacts --mailbox alice
pandoctl verify-contact --mailbox alice --contact bob --fingerprint <bob-fingerprint>
```

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
pandoctl init --mailbox alice --root-dir /media/usb/pando
pando --mailbox alice --to bob --root-dir /media/usb/pando
pando-relay --root-dir /media/usb/pando
```

More specific overrides still win when needed:

- `pando` and `pandoctl`: `-data-dir`
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

And creates a GitHub Release with binaries for Linux, macOS, and Windows. Version bumps follow conventional commits: `feat:` → minor, everything else → patch, `!` or `BREAKING CHANGE` → major.

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
