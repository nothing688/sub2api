package service

import (
	"context"
	"strings"
	"time"
)

// Spending-limit is recoverable at the end of the observed billing period.
// When no billing snapshot is available, use a short probe rather than
// fabricating a 24h boundary from the error arrival time.
const grokSpendingLimitProbeCooldown = 10 * time.Minute

// grokSpendingLimitMaxCooldown caps how far a spending-limit cooldown may
// reach into the future. Free-tier (monthly_limit=0) accounts share an xAI
// billing period, so taking the raw billing_period_end would lock them out for
// the entire remaining month (up to ~30 days). Clamping to a short horizon
// lets the pool retry periodically instead of parking the account until the
// period boundary.
const grokSpendingLimitMaxCooldown = 2 * time.Hour

// grokSpendingLimitMaxCooldownEnv overrides grokSpendingLimitMaxCooldown in
// seconds (e.g. "3600"). Invalid or unset values fall back to the default.
const grokSpendingLimitMaxCooldownEnv = "GROK_SPENDING_LIMIT_MAX_COOLDOWN_SECONDS"

func grokSpendingLimitResetAt(account *Account, now time.Time) time.Time {
	maxHorizon := now.Add(grokRateLimitCooldownOverride(grokSpendingLimitMaxCooldownEnv, grokSpendingLimitMaxCooldown))
	if account != nil {
		if billing, err := grokBillingSnapshotFromExtra(account.Extra); err == nil && billing != nil {
			for _, raw := range []string{billing.PeriodEnd, billing.BillingPeriodEnd} {
				if resetAt, err := time.Parse(time.RFC3339, strings.TrimSpace(raw)); err == nil && resetAt.After(now) {
					if resetAt.After(maxHorizon) {
						resetAt = maxHorizon
					}
					return resetAt
				}
			}
		}
	}
	return now.Add(grokSpendingLimitProbeCooldown)
}

// clearGrokNeedsReauthExtra drops the soft reauth flag after successful refresh
// or reauth. Best-effort; never fails the request path.
func clearGrokNeedsReauthExtra(ctx context.Context, repo AccountRepository, accountID int64) {
	if repo == nil || accountID <= 0 {
		return
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	_ = repo.UpdateExtra(stateCtx, accountID, map[string]any{
		"grok_needs_reauth":        false,
		"grok_needs_reauth_reason": "",
		"grok_needs_reauth_at":     "",
	})
}

func accountGrokNeedsReauth(account *Account) bool {
	if account == nil {
		return false
	}
	if account.Status == StatusError {
		msg := strings.ToLower(account.ErrorMessage)
		if strings.Contains(msg, "spending limit") || strings.Contains(msg, "reauthorize") {
			return true
		}
	}
	if v, ok := account.Extra["grok_needs_reauth"].(bool); ok && v {
		return true
	}
	if s, ok := account.Extra["grok_needs_reauth"].(string); ok {
		return strings.EqualFold(s, "true") || s == "1"
	}
	return false
}
