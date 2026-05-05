package tools

import (
	"fmt"
	"strings"
	"testing"
)

func TestAddLineNumbers(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "single line no newline",
			input: "hello",
			want:  "1: hello",
		},
		{
			name:  "single line with newline",
			input: "hello\n",
			want:  "1: hello\n",
		},
		{
			name:  "two lines no trailing newline",
			input: "a\nb",
			want:  "1: a\n2: b",
		},
		{
			name:  "two lines with trailing newline",
			input: "a\nb\n",
			want:  "1: a\n2: b\n",
		},
		{
			name:  "padding at 10 lines",
			input: strings.Repeat("x\n", 10),
			want: " 1: x\n 2: x\n 3: x\n 4: x\n 5: x\n" +
				" 6: x\n 7: x\n 8: x\n 9: x\n10: x\n",
		},
		{
			name:  "padding at 100 lines",
			input: strings.Repeat("x\n", 100),
			want: func() string {
				var b strings.Builder
				for i := 1; i <= 100; i++ {
					b.WriteString(fmt.Sprintf("%3d: x\n", i))
				}
				return b.String()
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := addLineNumbers(tc.input)
			if got != tc.want {
				t.Errorf("addLineNumbers(%q)\ngot:  %q\nwant: %q", tc.input, got, tc.want)
			}
		})
	}
}
