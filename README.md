# pando

<picture>
  <source srcset="docs/images/pando.webp" type="image/webp">
  <img src="docs/images/pando.webp" alt="Pando banner" width="100%">
</picture>

Private, encrypted terminal chat. End-to-end encrypted messages between people you trust, delivered through a self-hosted relay.

## Run

Start the relay:

```bash
go run ./cmd/pando-relay
```

Initialize an identity and exchange contacts:

```bash
go run ./cmd/pandoctl init --mailbox alice
go run ./cmd/pandoctl invite-code --mailbox alice --copy
go run ./cmd/pandoctl add-contact --mailbox alice --code '<bob-invite-code>'
go run ./cmd/pandoctl list-contacts --mailbox alice
go run ./cmd/pandoctl verify-contact --mailbox alice --contact bob --fingerprint <bob-fingerprint>
```

Start the client:

```bash
go run ./cmd/pando
```

### Relay configuration

Relay settings can be set via flags or environment variables — useful for container deployments:

```bash
export PANDO_RELAY_AUTH_TOKEN="your-shared-secret"
export PANDO_RELAY_ADDR=":8080"
go run ./cmd/pando-relay
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
