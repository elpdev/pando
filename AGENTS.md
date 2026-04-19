# AGENTS.md

Guidelines for AI agents working in the Pando codebase.

## Project Overview

Pando is a terminal-native encrypted chat system in Go with two binaries:

- `pando` — Bubble Tea TUI chat client and CLI for identity, contact, and device management
- `pando-relay` — WebSocket relay server with durable mailbox queues

## Essential Commands

```bash
# Run tests
go test ./...

# Run the relay
go run ./cmd/pando-relay

# Run the TUI client
go run ./cmd/pando --mailbox alice --to bob

# Run the management CLI
go run ./cmd/pando identity init --mailbox alice

# Build all binaries (used in CI)
CGO_ENABLED=0 go build -mod=vendor -o pando ./cmd/pando
CGO_ENABLED=0 go build -mod=vendor -o pando-relay ./cmd/pando-relay

# Docker build (uses vendored deps, no network fetch)
docker build -t pando-relay .
```

## Project Structure

```
cmd/                    thin main.go wrappers; all logic lives in internal/
  pando/                TUI client + CLI entrypoint → internal/clientcmd / internal/ctlcmd
  pando-relay/          relay entrypoint → internal/relaycmd

internal/
  clientcmd/            flag parsing, store init, wire chat model + WS client
  relaycmd/             flag/env parsing, Bolt queue store init, start HTTP server
  ctlcmd/               manual subcommand router (no Cobra), identity/contact ops
  config/               shared config structs, validation, env var loading
  identity/             Ed25519 + NaCl box crypto, device bundles, enrollment
  protocol/             relay/client wire types (Envelope, Message, etc.)
  session/              E2E encrypt/decrypt using box + Ed25519 signatures
  messaging/            high-level message orchestration, contact updates, acks
  store/                filesystem persistence (JSON + AES-GCM encrypted files)
  transport/            transport.Event + Client interface
  transport/ws/         Gorilla WebSocket transport implementation
  ui/                   top-level Bubble Tea app wrapper
  ui/chat/              chat Bubble Tea model (reconnect, send, receive, history)
  relay/                HTTP/WebSocket server, rate limiting, queue store backends
  logging/              slog factory
```

## Architecture & Data Flow

### Client → Relay → Client

1. `chat.Model` (Bubble Tea) receives user input
2. Calls `messaging.Service.EncryptOutgoing()` which:
   - Prepends contact-update envelopes to all recipient devices
   - Encrypts chat payload via `session.Encrypt()` (NaCl box, fan-out per device)
3. Sends via `transport/ws.Client` over WebSocket to relay
4. Relay queues if recipient offline, delivers live if subscribed
5. Recipient `chat.Model` receives via `transport.Event` channel
6. `messaging.Service.HandleIncoming()` decrypts, deduplicates, saves history
7. Auto-sends delivery ack back to sender

### Key Architectural Decisions

- **Transport is abstracted**: `transport.Client` interface with `Events() <-chan Event`. Only WebSocket impl exists.
- **Relay knows nothing about crypto**: it sees only mailbox IDs and opaque ciphertext.
- **Every outgoing batch prepends contact updates**: this keeps device bundles in sync automatically during normal messaging.
- **Control messages are plaintext envelopes** with `BodyEncoding` set to `contact-update-v1` or `delivery-ack-v1`.
- **Deduplication by envelope ID**: `store.ClientStore` tracks seen envelope IDs in an encrypted file.
- **History is per-peer, encrypted**: one AES-GCM encrypted file per peer mailbox under `~/.local/share/pando/<mailbox>/`.

## Crypto & Identity

- **Ed25519** for account and device signing
- **NaCl `box`** (Curve25519 + XSalsa20 + Poly1305) for E2E encryption
- **Fingerprints**: truncated SHA-256 hex of account signing public key (8 bytes = 16 hex chars)
- **Device bundles**: signed by account key; verified on import
- **Enrollment**: new device generates keys, creates `PendingEnrollment`, gets `EnrollmentRequest`. Existing device `Approve()`s it, encrypting account private key via `box.SealAnonymous()` to the new device's public key.
- **Signatures cover canonical string**: `signingBytes()` concatenates specific envelope fields with newlines. Changing field order breaks verification.

## Store & Persistence

### Client Store (`internal/store`)

All under `~/.local/share/pando/<mailbox>/` (or `-data-dir`):

- `identity.json` — identity.Device with private keys (0o600)
- `contacts.json` — map[accountID]Contact (0o600)
- `pending-enrollment.json` — pending enrollment state
- `history-<peer>.enc` — AES-GCM encrypted chat history
- `seen-envelopes.enc` — AES-GCM encrypted dedup set

Encryption key for `.enc` files: `SHA256("pando-history-v1" + accountSigningPrivate)`.

### Relay Store (`internal/relay`)

- `BoltQueueStore`: BBolt DB, JSON-serialized envelope queues per mailbox
- `MemoryQueueStore`: in-memory map for tests

## Configuration

### Client

Flags: `-relay`, `-relay-token`, `-mailbox`, `-to`, `-data-dir`
Defaults: Relay URL `ws://localhost:8080/ws`, data dir auto-computed from mailbox.

### Relay

Flags: `-addr`, `-store`, `-ttl`, `-max-message-bytes`, `-max-queued-messages`, `-max-queued-bytes`, `-rate-limit-per-minute`, `-auth-token`, `-landing-page`
Env vars (prefixed `PANDO_RELAY_`): `ADDR`, `STORE_PATH`, `AUTH_TOKEN`, `QUEUE_TTL`, `MAX_MESSAGE_BYTES`, `MAX_QUEUED_MESSAGES`, `MAX_QUEUED_BYTES`, `RATE_LIMIT_PER_MINUTE`, `ALLOWED_ORIGINS`, `LANDING_PAGE`
CLI flags take precedence over env vars. Relay applies env first, then overrides with flags.

## Testing Conventions

- **No external test libraries** (no testify). Use `t.Fatalf("context: %v", err)`.
- **Table-driven tests are rare**; prefer linear step-by-step "round-trip" tests.
- **Real dependencies in tests**: real BoltDB on `t.TempDir()`, real `identity.New()`, real crypto. No mocks.
- **HTTP/WebSocket tests**: spin up `httptest.NewServer` with real relay server, dial with Gorilla WebSocket.
- **Custom test logger**: `testWriter` type routes slog output to `t.Log`.
- **Helper functions**: mark with `t.Helper()`, e.g. `newTestServer`, `dialTestConn`, `writeMessage`, `readMessage`.
- **Time injection**: pass `time.Time` into functions that need determinism (e.g. rate limiter).

Example test pattern from `internal/relay/server_test.go`:

```go
func TestQueuedMessageDeliveredOnSubscribe(t *testing.T) {
    server := newTestServer(t)
    publisher := dialTestConn(t, server)
    // ... writeMessage, readMessage assertions
}
```

## Code Style & Conventions

- **Standard Go formatting**: `gofmt` / `goimports`
- **Error wrapping**: always wrap with context, e.g. `fmt.Errorf("decode queue: %w", err)`
- **Defensive copying**: identity package uses `append(ed25519.PublicKey(nil), ...)` and `append([]byte(nil), ...)` to deep-copy slices before returning.
- **JSON files**: `json.MarshalIndent` with `"  "` for human-readable plaintext before encryption.
- **File permissions**: `0o600` for sensitive files, `0o700` for directories.
- **No `internal/app/`**: directory exists but is empty. Do not add code there.

## Important Gotchas

### WebSocket Client Reconnect

`transport/ws.Client.Connect()` closes the previous connection if one exists. The `chat.Model` handles reconnect with exponential backoff (capped at 16s) via `reconnectCmd()`. On reconnect, it re-subscribes automatically.

### Event Channel Safety

`ws.Client.sendEvent()` uses `recover()` to prevent panic when sending to a closed channel. The events channel is closed in `Close()`.

### Mailbox Affinity

`store.LoadOrCreateIdentity()` enforces that the store's current device mailbox matches the requested mailbox. Mismatch returns an error. This prevents accidentally mixing identities.

### Contact Updates Preserve Verification

When a contact update arrives, `messaging.applyContactUpdate()` copies `existing.Verified` to the updated contact before saving. Verification is never lost during automatic device bundle refresh.

### Delivery Acks

`chat.Model` sends delivery acks automatically for successfully decrypted messages. The ack updates the sender's local history via `MarkHistoryDelivered()`. Empty `ClientMessageID` skips ack generation.

### Rate Limiting

Relay uses a per-mailbox sliding-window rate limiter. The window resets after 1 minute of inactivity. Envelope size is computed as `len(Body) + len(Ciphertext) + len(Nonce) + len(Signature)`.

### Queue Expiration

`filterExpired()` uses an in-place slice re-slicing trick (`queue[:0]`) for zero-allocation filtering. Called on both read and write paths.

### Invite Codes

Invite codes are base64-raw-URL-encoded JSON of `identity.InviteBundle`. They are not encrypted — they contain only public keys. The security property comes from fingerprint verification out-of-band.

### Docker / CI

- The project vendors dependencies (`vendor/`). Docker build uses `-mod=vendor`.
- CI skips releases for doc-only changes (`.md`, `docs/`, `.github/`, etc.).
- Conventional commits drive semver: `feat:` → minor, `fix:` → patch, `!` or `BREAKING CHANGE:` → major.

## Dependencies

Key external deps:

- `github.com/charmbracelet/bubbletea` — TUI framework
- `github.com/charmbracelet/bubbles` — textinput, viewport components
- `github.com/charmbracelet/lipgloss` — styling
- `github.com/gorilla/websocket` — WebSocket client/server
- `go.etcd.io/bbolt` — embedded KV store for relay queues
- `golang.org/x/crypto/nacl/box` — E2E encryption
- `github.com/google/uuid` — device IDs
- `github.com/atotto/clipboard` — invite code copy in pando
