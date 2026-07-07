package cli

import "testing"

// TestStripOneTrailingNewline 锁定「剥恰好一个尾随换行」的语义：
// 仅剥末尾的一个 \n，内部换行与多余尾换行不动——与 node show --prompt「补恰好一个」配对，保证 round-trip 字节稳定。
func TestStripOneTrailingNewline(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"无尾换行原样", "abc", "abc"},
		{"恰一个尾换行剥掉", "abc\n", "abc"},
		{"多个尾换行只剥一个", "abc\n\n", "abc\n"},
		{"空输入", "", ""},
		{"内部换行不动", "a\nb\n", "a\nb"},
		{"仅一个换行", "\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripOneTrailingNewline([]byte(tc.in)); got != tc.want {
				t.Fatalf("stripOneTrailingNewline(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestStripThenAppendNewlineRoundTrip 锁定剥/补成对性（design D7）：
// 对不以换行结尾的提示词，stripOneTrailingNewline(x) 再补一个 "\n" 应等于 x + "\n"（show 侧的补换行输出），
// 即 set-prompt(show(x)) 得回 x —— round-trip 字节稳定的核心不变式。
func TestStripThenAppendNewlineRoundTrip(t *testing.T) {
	inputs := []string{"abc", "a\nb", "含 {{gen}} 的多行\n提示词", ""}
	for _, x := range inputs {
		shown := x + "\n" // show --prompt：输出 promptTemplate 后补恰好一个 \n
		if got := stripOneTrailingNewline([]byte(shown)); got != x {
			t.Fatalf("round-trip 破坏：stripOneTrailingNewline(%q) = %q, want %q", shown, got, x)
		}
	}
}
