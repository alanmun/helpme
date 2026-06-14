package main

import "testing"

func TestParseFix(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantCmd string
		wantErr bool
	}{
		{"plain json", `{"cmd":"find . -name x","why":"use -name"}`, "find . -name x", false},
		{"code fence", "```json\n{\"cmd\":\"ls -la\",\"why\":\"long form\"}\n```", "ls -la", false},
		{"prose wrapped", `Sure! {"cmd":"grep -r foo .","why":"recurse"} hope that helps`, "grep -r foo .", false},
		{"newline in cmd collapses", "{\"cmd\":\"echo \\na\",\"why\":\"x\"}", "echo  a", false},
		{"no json", `I cannot help with that`, "", true},
		{"empty cmd", `{"cmd":"","why":"nothing"}`, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseFix(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got cmd=%q", got.Cmd)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Cmd != c.wantCmd {
				t.Fatalf("cmd = %q, want %q", got.Cmd, c.wantCmd)
			}
		})
	}
}
