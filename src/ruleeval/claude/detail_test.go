package claude

import "testing"

// TestWireDetail_Empty checks each way a detail can say nothing on its
// own: a blank explanation, or a confidence of 0 or less, whatever the
// other field holds (strspc-manager#338).
func TestWireDetail_Empty(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    wireDetail
		want bool
	}{
		{"filled", wireDetail{Verdict: "violated", Explanation: "No test file.", Confidence: 0.6}, false},
		{"blank explanation", wireDetail{Verdict: "violated", Explanation: "", Confidence: 0.6}, true},
		{"whitespace explanation", wireDetail{Verdict: "violated", Explanation: " \n\t", Confidence: 0.6}, true},
		{"zero confidence", wireDetail{Verdict: "violated", Explanation: "No test file.", Confidence: 0}, true},
		{"negative confidence", wireDetail{Verdict: "violated", Explanation: "No test file.", Confidence: -0.1}, true},
		{"both", wireDetail{Verdict: "violated"}, true},
	} {
		if got := tc.d.empty(); got != tc.want {
			t.Errorf("%s: empty() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
