package identity

const (
	TrustSourceUnverified     = "unverified"
	TrustSourceManualVerified = "manual-verified"
	TrustSourceRelayDirectory = "relay-directory"
	TrustSourceInviteCode     = "invite-code"
)

func TrustRank(source string) int {
	switch source {
	case TrustSourceManualVerified:
		return 3
	case TrustSourceRelayDirectory, TrustSourceInviteCode:
		return 2
	case TrustSourceUnverified, "":
		return 1
	default:
		return 0
	}
}

func StrongerTrust(current, next string) string {
	if TrustRank(next) > TrustRank(current) {
		return next
	}
	return current
}

func (c *Contact) NormalizeTrust() {
	if c == nil {
		return
	}
	if c.Verified {
		if c.TrustSource == "" || c.TrustSource == TrustSourceUnverified {
			c.TrustSource = TrustSourceManualVerified
		}
		return
	}
	if c.TrustSource == "" {
		c.TrustSource = TrustSourceUnverified
	}
}

func TrustLabel(source string, verified bool) string {
	switch source {
	case TrustSourceManualVerified:
		return "verified"
	case TrustSourceRelayDirectory:
		return "relay"
	case TrustSourceInviteCode:
		return "invite"
	default:
		if verified {
			return "verified"
		}
		return "unverified"
	}
}
