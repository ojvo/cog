package csv

import "testing"

func TestToSnake(t *testing.T) {
	cases := []struct {
		in        string
		screaming bool
		want      string
	}{
		{"", false, ""},
		{"", true, ""},
		{"simple", false, "simple"},
		{"simple", true, "SIMPLE"},
		{"CamelCase", false, "camel_case"},
		{"CamelCase", true, "CAMEL_CASE"},
		{"UserID", false, "user_id"},
		{"UserID", true, "USER_ID"},
		{"HTTP2Server", false, "http2_server"},
		{"HTTP2Server", true, "HTTP2_SERVER"},
		{"user-id", false, "user_id"},
		{"user_id", false, "user_id"},
		{"user name", false, "user_name"},
		{" already trimmed ", false, "already_trimmed"},
		{"MixedCaseWithABC", false, "mixed_case_with_abc"},
		{"snake_case_input", false, "snake_case_input"},
		{"SNAKE_CASE", false, "snake_case"},
		{"SNAKE_CASE", true, "SNAKE_CASE"},
	}
	for _, c := range cases {
		got := ToSnake(c.in, c.screaming)
		if got != c.want {
			t.Errorf("ToSnake(%q, %v) = %q, want %q", c.in, c.screaming, got, c.want)
		}
	}
}
