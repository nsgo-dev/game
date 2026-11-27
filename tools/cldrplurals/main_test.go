package main

import (
	"slices"
	"testing"
)

func TestCompileIntegerCondition(t *testing.T) {
	tests := []struct {
		name string
		cond string
		want string
	}{
		{
			name: "empty is true",
			cond: "",
			want: "true",
		},
		{
			name: "decimal only v != 0 simplifies to false",
			cond: "v != 0",
			want: "",
		},
		{
			name: "decimal only f != 0 simplifies to false",
			cond: "f != 0",
			want: "",
		},
		{
			name: "english i = 1 and v = 0",
			cond: "i = 1 and v = 0",
			want: "n == 1",
		},
		{
			name: "french i = 0,1 collapses with range merge",
			cond: "i = 0,1",
			want: "n <= 1",
		},
		{
			name: "portuguese i = 0..1 matches french",
			cond: "i = 0..1",
			want: "n <= 1",
		},
		{
			name: "french many with compact exponent",
			cond: "e = 0 and i != 0 and i % 1000000 = 0 and v = 0 or e != 0..5",
			want: "(n != 0 && n%1000000 == 0)",
		},
		{
			name: "czech few",
			cond: "i = 2..4 and v = 0",
			want: "(n >= 2 && n <= 4)",
		},
		{
			name: "icelandic drops decimal branch",
			cond: "t = 0 and i % 10 = 1 and i % 100 != 11 or t % 10 = 1 and t % 100 != 11",
			want: "(n%10 == 1 && n%100 != 11)",
		},
		{
			name: "romanian few drops v != 0",
			cond: "v != 0 or n = 0 or n != 1 and n % 100 = 1..19",
			want: "n == 0 || (n != 1 && (n%100 >= 1 && n%100 <= 19))",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compileIntegerCondition(tc.cond)
			if err != nil {
				t.Fatalf("compileIntegerCondition(%q): %v", tc.cond, err)
			}
			if got != tc.want {
				t.Fatalf("compileIntegerCondition(%q) = %q, want %q", tc.cond, got, tc.want)
			}
		})
	}
}

func TestParseIntegerSamples(t *testing.T) {
	got, err := parseIntegerSamples("0, 2~4, 100, 1c6, 2c3, …")
	if err != nil {
		t.Fatalf("parseIntegerSamples: %v", err)
	}
	want := []int{0, 2, 3, 4, 100, 1000000, 2000}
	if !slices.Equal(got, want) {
		t.Fatalf("parseIntegerSamples = %v, want %v", got, want)
	}
}
