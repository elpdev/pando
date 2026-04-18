# chatui

First vertical slice for a terminal-native chat client and relay in Go.

Current scope:

- WebSocket relay with durable mailbox delivery
- Bubble Tea shell app with a dedicated chat route model
- Invite-based contact exchange and verified device bundles
- Encrypted 1:1 message envelopes between mailbox IDs
- Encrypted local chat history per peer conversation
- Relay size, TTL, and rate-limit controls
- Client reconnect with backoff after relay disconnects
- Contact fingerprint display and explicit verification flow
- Optional relay auth tokens for private deployments
- Trusted device enrollment, listing, and revocation

## Project Shape

- `cmd/chatui`: thin client entrypoint
- `cmd/chatui-relay`: thin relay entrypoint
- `cmd/chatuictl`: local identity and contact management
- `internal/clientcmd`: client startup and flag wiring
- `internal/ctlcmd`: control command wiring
- `internal/relaycmd`: relay startup and flag wiring
- `internal/config`: shared runtime config types and validation
- `internal/identity`: account and device key material
- `internal/logging`: shared logger setup
- `internal/messaging`: client-side message preparation and decode logic
- `internal/protocol`: relay/client wire types
- `internal/session`: encrypted message envelope helpers
- `internal/store`: local identity and contact persistence
- `internal/transport`: transport interface boundary
- `internal/transport/ws`: WebSocket transport implementation
- `internal/ui`: shell-level Bubble Tea app
- `internal/ui/chat`: chat route model
- `internal/relay`: relay server and mailbox behavior

## Run

Start the relay:

```bash
go run ./cmd/chatui-relay
```

Optional hardening flags:

```bash
go run ./cmd/chatui-relay --ttl 24h --max-message-bytes 65536 --rate-limit-per-minute 120 --auth-token secret-token
```

Initialize Alice and Bob locally and exchange invites:

```bash
go run ./cmd/chatuictl --help
go run ./cmd/chatuictl init --mailbox alice
go run ./cmd/chatuictl init --mailbox bob
go run ./cmd/chatuictl export-invite --mailbox alice --out /tmp/alice-invite.json
go run ./cmd/chatuictl export-invite --mailbox bob --out /tmp/bob-invite.json
go run ./cmd/chatuictl import-contact --mailbox alice --invite /tmp/bob-invite.json
go run ./cmd/chatuictl import-contact --mailbox bob --invite /tmp/alice-invite.json
go run ./cmd/chatuictl list-contacts --mailbox alice
go run ./cmd/chatuictl verify-contact --mailbox alice --contact bob --fingerprint <bob-fingerprint>
```

Enroll a second trusted device for Alice:

```bash
go run ./cmd/chatuictl create-enrollment --account alice --mailbox alice-phone --out /tmp/alice-phone-request.json
go run ./cmd/chatuictl approve-enrollment --mailbox alice --request /tmp/alice-phone-request.json --out /tmp/alice-phone-approval.json
go run ./cmd/chatuictl complete-enrollment --mailbox alice-phone --approval /tmp/alice-phone-approval.json
go run ./cmd/chatuictl list-devices --mailbox alice
```

Revoke a trusted device:

```bash
go run ./cmd/chatuictl revoke-device --mailbox alice --device alice-phone
```

Open one terminal for Alice:

```bash
go run ./cmd/chatui --mailbox alice --to bob
go run ./cmd/chatui --mailbox alice --to bob --relay-token secret-token
```

Open another terminal for Bob:

```bash
go run ./cmd/chatui --mailbox bob --to alice
```

If Bob is offline when Alice sends a message, the relay will keep that ciphertext in its local queue store and deliver it when Bob subscribes.

## Current Limitations

- Contact device updates still depend on re-importing an updated invite bundle
- No automatic history sync to newly enrolled devices
