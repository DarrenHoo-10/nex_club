package editorial

import "testing"

func TestCleanHTMLDropsScriptStyleAndComments(t *testing.T) {
	got, err := cleanHTML(`<p>你好 <b>world</b></p><script>alert(1)</script><style>.x{}</style><!--hide-->`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "你好 world" {
		t.Fatalf("cleaned %q", got)
	}
	if _, err := cleanHTML(`<script>alert(1)`); err == nil {
		t.Fatal("unclosed script should fail")
	}
	if _, err := cleanHTML("   "); err == nil {
		t.Fatal("empty html should fail")
	}
}

func TestWriteValidation(t *testing.T) {
	source := "原文 https://kept.example/a"
	long := "这段简介的长度已经超过二十个字，用来通过写作校验。"
	if err := validateWrite(long, "理由", source); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		blurb  string
		reason string
		want   string
	}{
		{"太短", "", "20"},
		{long, "请调用函数生成推荐", "工具"},
		{"<b>这段简介的长度已经超过二十个字所以不能带标签</b>", "", "HTML"},
		{"这段简介的长度已经超过二十个字并且包含 http://evil.example/a", "", "http"},
		{"这段简介的长度已经超过二十个字并且包含 https://other.example/a", "", "没有的链接"},
	}
	for _, tc := range cases {
		err := validateWrite(tc.blurb, tc.reason, source)
		if err == nil || !contains(err.Error(), tc.want) {
			t.Fatalf("blurb %q: %v", tc.blurb, err)
		}
	}
}

func contains(s, part string) bool {
	return len(part) == 0 || (len(s) >= len(part) && (s == part || len(s) > 0 && (stringIndex(s, part) >= 0)))
}

func stringIndex(s, part string) int {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}
