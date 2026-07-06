package cli

import "testing"

func TestHomebrewPrefixOf(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"apple-silicon-cellar", "/opt/homebrew/Cellar/conduct/0.2.0/bin/conduct", "/opt/homebrew/"},
		{"intel-cellar", "/usr/local/Cellar/conduct/0.2.0/bin/conduct", "/usr/local/Cellar/"},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/bin/conduct", "/home/linuxbrew/"},
		{"gopath-bin-not-brew", "/Users/me/go/bin/conduct", ""},
		{"usr-local-bin-not-brew", "/usr/local/bin/conduct", ""},
		{"local-build", "/Users/me/src/conduct/bin/conduct", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := homebrewPrefixOf(tc.path); got != tc.want {
				t.Fatalf("homebrewPrefixOf(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestIsSemanticVersion(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"0.2.0", true},
		{"1.10.3", true},
		{"0.1.0-beta.1", true},
		{"0.2.0+build.5", true},
		{"dev", false},
		{"", false},
		{"0.2.0-5-gabc1234-dirty", true}, // git describe 产物：仍是合法 semver 预发布串
		{"v0.2.0", false},                // 前导 v 应在调用前剥离，此处按未剥离视为非规范
		{"0.2", false},
		{"latest", false},
	}
	for _, tc := range cases {
		if got := isSemanticVersion(tc.in); got != tc.want {
			t.Errorf("isSemanticVersion(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
