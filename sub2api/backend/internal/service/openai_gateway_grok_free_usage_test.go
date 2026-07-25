package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestParseGrokActualLimitTokens(t *testing.T) {
	a, b, ok := parseGrokActualLimitTokens(`tokens (actual/limit): 1124730/1000000`)
	require.True(t, ok)
	require.EqualValues(t, 1124730, a)
	require.EqualValues(t, 1000000, b)
}

func TestParseGrokFreeUsageExhaustedBody(t *testing.T) {
	body := []byte(`{"code":"subscription:free-usage-exhausted","error":"You've used all the included free usage for model grok-4.5-build-free for now. Usage resets over a rolling 24-hour window — tokens (actual/limit): 1124730/1000000."}`)
	snap := parseGrokFreeUsageExhaustedBody(body, time.Now())
	require.NotNil(t, snap)
	require.NotNil(t, snap.Tokens)
	require.NotNil(t, snap.Tokens.Remaining)
	require.EqualValues(t, 0, *snap.Tokens.Remaining)
	require.NotNil(t, snap.Tokens.Limit)
	require.EqualValues(t, 1000000, *snap.Tokens.Limit)
}

func TestIsGrokFreeUsageExhaustedBody(t *testing.T) {
	require.True(t, isGrokFreeUsageExhaustedBody([]byte(`subscription:free-usage-exhausted`)))
	require.False(t, isGrokFreeUsageExhaustedBody([]byte(`permission-denied`)))
}

func TestGrokRateLimitResetAtForAccountKeepsFreeUsage24h(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	resetUnix := now.Add(24 * time.Hour).Unix()
	zero := int64(0)
	limit := int64(1_000_000)
	snapshot := &xai.QuotaSnapshot{
		StatusCode:        429,
		ObservationSource: "free_usage_exhausted_body",
		Tokens: &xai.QuotaWindow{
			Limit:     &limit,
			Remaining: &zero,
			ResetUnix: &resetUnix,
			ResetAt:   now.Add(24 * time.Hour).Format(time.RFC3339),
		},
		UpdatedAt: now.Format(time.RFC3339),
	}
	account := &Account{ID: 9001, Platform: PlatformGrok, Type: AccountTypeOAuth}
	// Force free-tier inference via token limit alone.
	account.Extra = map[string]any{
		"grok_usage_snapshot": snapshot,
	}
	resetAt, limited := grokRateLimitResetAtForAccount(account, snapshot, now)
	require.True(t, limited)
	require.True(t, resetAt.After(now.Add(30*time.Minute)), "free-usage must not clamp to short RPM max, got %v", resetAt.Sub(now))
	require.WithinDuration(t, now.Add(24*time.Hour), resetAt, time.Second)
}

func TestShouldStopOpenAIOAuth429FailoverKeepsSwitchingOnFreeUsage(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 44, Platform: PlatformGrok, Type: AccountTypeOAuth}
	body := []byte(`{"code":"subscription:free-usage-exhausted","error":"You've used all the included free usage"}`)
	var state OpenAIOAuth429FailoverState
	require.False(t, svc.ShouldStopOpenAIOAuth429Failover(account, 429, 1, &state, body))
	require.True(t, state.freeUsageExhaustedSeen)
	// Subsequent non-body failures still keep switching once free-usage was seen.
	require.False(t, svc.ShouldStopOpenAIOAuth429Failover(account, 500, 2, &state))
	require.False(t, svc.ShouldStopOpenAIOAuth429Failover(account, 429, 5, &state, body))
}
