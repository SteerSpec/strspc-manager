package claude

import (
	"strings"
	"testing"
)

// TestEstimateTokens_Ratio checks the estimate against the ratio it
// assumes: rounded up, so a byte never costs nothing, and bytesForTokens its
// inverse, so a part sized in bytes from a token budget stays within that
// budget. It holds whatever ratio is set.
func TestEstimateTokens_Ratio(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 4, 5, 6, 3907, 1 << 20} {
		got := estimateTokensForBytes(n)
		if want := EstimateTokens(strings.Repeat("x", n)); got != want {
			t.Errorf("estimateTokensForBytes(%d) = %d, EstimateTokens = %d", n, got, want)
		}
		if exact := float64(n) * bytesPerTokenDen / bytesPerTokenNum; float64(got) < exact || float64(got) >= exact+1 {
			t.Errorf("estimateTokensForBytes(%d) = %d, want %v rounded up", n, got, exact)
		}
		if b := bytesForTokens(n); estimateTokensForBytes(b) > n || estimateTokensForBytes(b+1) <= n {
			t.Errorf("bytesForTokens(%d) = %d is not the most bytes that %d tokens hold", n, b, n)
		}
	}
}
