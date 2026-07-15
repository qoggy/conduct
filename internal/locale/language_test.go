package locale

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  Language
	}{
		{name: "zh", value: "zh", want: Chinese},
		{name: "region and encoding", value: "zh_CN.UTF-8", want: Chinese},
		{name: "script", value: "zh-Hans", want: Chinese},
		{name: "case insensitive", value: "ZH_TW", want: Chinese},
		{name: "english", value: "en_US.UTF-8", want: English},
		{name: "C", value: "C", want: English},
		{name: "POSIX", value: "POSIX", want: English},
		{name: "unknown", value: "fr_FR.UTF-8", want: English},
		{name: "invalid chinese prefix", value: "zhongwen", want: English},
		{name: "empty", value: "", want: English},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Parse(test.value); got != test.want {
				t.Fatalf("Parse(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestDetectPriority(t *testing.T) {
	tests := []struct {
		name     string
		all      string
		messages string
		lang     string
		want     Language
	}{
		{name: "LC_ALL", all: "zh_CN", messages: "en_US", lang: "en_US", want: Chinese},
		{name: "LC_MESSAGES", messages: "zh_CN", lang: "en_US", want: Chinese},
		{name: "LANG", lang: "zh_CN", want: Chinese},
		{name: "unknown high priority falls back to english", all: "fr_FR", messages: "zh_CN", want: English},
		{name: "C high priority falls back to english", all: "C", messages: "zh_CN", want: English},
		{name: "POSIX high priority falls back to english", messages: "POSIX", lang: "zh_CN", want: English},
		{name: "unset", want: English},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("LC_ALL", test.all)
			t.Setenv("LC_MESSAGES", test.messages)
			t.Setenv("LANG", test.lang)
			if got := Detect(); got != test.want {
				t.Fatalf("Detect() = %v, want %v", got, test.want)
			}
		})
	}
}
