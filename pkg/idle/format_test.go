package idle_test

import (
	"testing"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/idle"
)

type formatCase struct {
	n    idle.Number
	want string
}

func checkFormat(t *testing.T, f idle.Format, cases []formatCase) {
	t.Helper()
	for _, c := range cases {
		if got := f.Format(c.n); got != c.want {
			t.Errorf("%v %d digits fixed=%v: Format(%v) = %q, want %q", f.Notation, f.Digits, f.Fixed, c.n, got, c.want)
		}
	}
}

func TestFormatShortEnglish(t *testing.T) {
	checkFormat(t, idle.Format{}, []formatCase{
		{idle.Number{}, "0"},
		{idle.New(1), "1"},
		{idle.New(12.345), "12.3"},
		{idle.New(999), "999"},
		{idle.New(999.4), "999"},
		{idle.New(999.6), "1K"},
		{idle.New(1000), "1K"},
		{idle.New(1234), "1.23K"},
		{idle.New(1200), "1.2K"},
		{idle.New(12345), "12.3K"},
		{idle.New(123456), "123K"},
		{idle.New(999999), "1M"},
		{idle.New(1e6), "1M"},
		{idle.New(1.5e9), "1.5B"},
		{idle.New(1e12), "1T"},
		{idle.New(4.56e15), "4.56Qa"},
		{idle.NewExp(1.23, 45), "1.23Qad"},
		{idle.NewExp(1, 63), "1Vg"},
		{idle.NewExp(9.99, 65), "999Vg"},
		{idle.NewExp(9.9999, 65), "1e66"},
		{idle.NewExp(1.23, 66), "1.23e66"},
		{idle.NewExp(1.23, 1000000), "1.23e1000000"},
		{idle.New(-1234), "-1.23K"},
		{idle.New(0.5), "0.5"},
		{idle.New(0.001234), "0.00123"},
		{idle.New(0.000001), "0.000001"},
		{idle.New(1.234e-7), "1.23e-7"},
	})
	checkFormat(t, idle.Format{Digits: 1}, []formatCase{
		{idle.New(1234), "1K"},
		{idle.New(1500), "2K"},
		{idle.New(950), "950"},
		{idle.New(9.6), "10"},
	})
	checkFormat(t, idle.Format{Digits: 4}, []formatCase{
		{idle.New(1234567), "1.235M"},
		{idle.New(3.14159), "3.142"},
	})
	checkFormat(t, idle.Format{Fixed: true}, []formatCase{
		{idle.Number{}, "0.00"},
		{idle.New(5), "5.00"},
		{idle.New(1200), "1.20K"},
		{idle.New(999.96), "1.00K"},
		{idle.New(12000), "12.0K"},
		{idle.New(250000), "250K"},
		{idle.NewExp(1, 70), "1.00e70"},
	})
}

func TestFormatShortChinese(t *testing.T) {
	zh := idle.Format{Locale: idle.ChineseSimplified()}
	checkFormat(t, zh, []formatCase{
		{idle.New(0), "0"},
		{idle.New(999), "999"},
		{idle.New(9999), "9,999"},
		{idle.New(10000), "1万"},
		{idle.New(12345), "1.23万"},
		{idle.New(99999), "10万"},
		{idle.New(123456), "12.3万"},
		{idle.New(1234567), "123万"},
		{idle.New(12345678), "1235万"},
		{idle.New(99999999), "1亿"},
		{idle.New(123456789), "1.23亿"},
		{idle.New(1e12), "1兆"},
		{idle.New(3.5e16), "3.5京"},
		{idle.New(1e20), "1垓"},
		{idle.NewExp(1.23, 45), "12.3载"},
		{idle.NewExp(1, 48), "1e48"},
		{idle.New(-56789), "-5.68万"},
		{idle.New(0.25), "0.25"},
	})
	hant := idle.Format{Locale: idle.ChineseTraditional()}
	checkFormat(t, hant, []formatCase{
		{idle.New(12345), "1.23萬"},
		{idle.New(123456789), "1.23億"},
		{idle.NewExp(5, 36), "5澗"},
	})
	checkFormat(t, idle.Format{Notation: idle.Full, Locale: idle.ChineseSimplified()}, []formatCase{
		{idle.New(1234567), "1,234,567"},
	})
}

func TestFormatScientificAndEngineering(t *testing.T) {
	checkFormat(t, idle.Format{Notation: idle.Scientific}, []formatCase{
		{idle.New(0), "0"},
		{idle.New(999), "999"},
		{idle.New(999.7), "1e3"},
		{idle.New(1234), "1.23e3"},
		{idle.NewExp(1.23, 45), "1.23e45"},
		{idle.NewExp(-9.876, 400), "-9.88e400"},
		{idle.New(1.5e-9), "1.5e-9"},
	})
	checkFormat(t, idle.Format{Notation: idle.Scientific, Fixed: true, Digits: 4}, []formatCase{
		{idle.NewExp(1.2, 45), "1.200e45"},
	})
	checkFormat(t, idle.Format{Notation: idle.Engineering}, []formatCase{
		{idle.New(512), "512"},
		{idle.New(1234), "1.23e3"},
		{idle.New(123456), "123e3"},
		{idle.New(999999), "1e6"},
		{idle.NewExp(1.23, 46), "12.3e45"},
		{idle.NewExp(1.23, 47), "123e45"},
	})
	de := idle.Format{Notation: idle.Scientific, Locale: mustLocale(t, deLocale)}
	checkFormat(t, de, []formatCase{
		{idle.NewExp(1.23, 45), "1,23E45"},
	})
}

func TestFormatLetters(t *testing.T) {
	checkFormat(t, idle.Format{Notation: idle.Letters}, []formatCase{
		{idle.New(999), "999"},
		{idle.New(1234), "1.23K"},
		{idle.New(1e6), "1M"},
		{idle.New(1e9), "1B"},
		{idle.New(1e12), "1T"},
		{idle.New(1e15), "1aa"},
		{idle.New(2.5e18), "2.5ab"},
		{idle.NewExp(1, 3*30), "1az"},
		{idle.NewExp(1, 3*31), "1ba"},
		{idle.NewExp(1, 3*(5+675)), "1zz"},
		{idle.NewExp(1, 3*(5+676)), "1aaa"},
		{idle.NewExp(4.2, 3*(5+676+17576)), "4.2aaaa"},
	})
}

func TestFormatFull(t *testing.T) {
	checkFormat(t, idle.Format{Notation: idle.Full}, []formatCase{
		{idle.New(0), "0"},
		{idle.New(12), "12"},
		{idle.New(1234), "1,234"},
		{idle.New(1234567), "1,234,567"},
		{idle.New(1234.5678), "1,235"},
		{idle.New(-987654321), "-987,654,321"},
		{idle.New(123456789012345678901), "123,456,789,012,345,680,000"},
		{idle.New(1e21), "1e21"},
	})
	checkFormat(t, idle.Format{Notation: idle.Full, Digits: 6}, []formatCase{
		{idle.New(1234.5678), "1,234.57"},
	})
	de := mustLocale(t, deLocale)
	checkFormat(t, idle.Format{Notation: idle.Full, Digits: 6, Locale: de}, []formatCase{
		{idle.New(1234.5), "1.234,5"},
	})
	checkFormat(t, idle.Format{Locale: de}, []formatCase{
		{idle.New(1500), "1,5 Tsd."},
		{idle.New(2.25e6), "2,25 Mio."},
		{idle.New(999), "999"},
	})
	noGroup := idle.English()
	noGroup.Group = ""
	checkFormat(t, idle.Format{Notation: idle.Full, Locale: noGroup}, []formatCase{
		{idle.New(1234567), "1234567"},
	})
	noUnits := idle.English()
	noUnits.Units = nil
	checkFormat(t, idle.Format{Locale: noUnits}, []formatCase{
		{idle.New(1234567), "1,234,567"},
		{idle.NewExp(1, 30), "1e30"},
	})
}

func TestNotationString(t *testing.T) {
	want := map[idle.Notation]string{idle.Short: "short", idle.Scientific: "scientific", idle.Engineering: "engineering", idle.Letters: "letters", idle.Full: "full", 42: "Notation(42)"}
	for n, s := range want {
		if got := n.String(); got != s {
			t.Errorf("Notation(%d).String() = %q, want %q", n, got, s)
		}
	}
}

func TestFormatAppendAllocatesNothing(t *testing.T) {
	buf := make([]byte, 0, 64)
	formats := []idle.Format{{}, {Notation: idle.Scientific}, {Notation: idle.Letters}, {Notation: idle.Full}, {Locale: idle.ChineseSimplified()}}
	n := idle.NewExp(1.2345, 47)
	for _, f := range formats {
		if allocs := testing.AllocsPerRun(100, func() { buf = f.Append(buf[:0], n) }); allocs != 0 {
			t.Errorf("%v Append allocates %v times", f.Notation, allocs)
		}
	}
}

func FuzzFormat(f *testing.F) {
	f.Add(1.5, int64(3), uint8(0), uint8(3), false)
	f.Add(9.9999, int64(65), uint8(0), uint8(3), true)
	f.Add(-1.0, int64(-7), uint8(1), uint8(17), false)
	f.Add(4.2, int64(1e15), uint8(3), uint8(0), false)
	f.Add(9.99999, int64(20), uint8(4), uint8(40), true)
	locales := []*idle.Locale{idle.English(), idle.ChineseSimplified(), idle.ChineseTraditional()}
	f.Fuzz(func(t *testing.T, m float64, e int64, notation uint8, digits uint8, fixed bool) {
		if m != m || m > 1e300 || m < -1e300 {
			return
		}
		n := idle.NewExp(m, e%idle.MaxExponent)
		for _, loc := range locales {
			out := idle.Format{Notation: idle.Notation(notation % 5), Digits: int(digits), Fixed: fixed, Locale: loc}.Format(n)
			if out == "" || !utf8.ValidString(out) {
				t.Fatalf("Format(%v) = %q", n, out)
			}
		}
	})
}

func BenchmarkFormatShort(b *testing.B) {
	b.ReportAllocs()
	buf := make([]byte, 0, 64)
	f := idle.Format{}
	n := idle.NewExp(1.2345, 47)
	for b.Loop() {
		buf = f.Append(buf[:0], n)
	}
}

func BenchmarkFormatFull(b *testing.B) {
	b.ReportAllocs()
	buf := make([]byte, 0, 64)
	f := idle.Format{Notation: idle.Full}
	n := idle.New(123456789012)
	for b.Loop() {
		buf = f.Append(buf[:0], n)
	}
}

// TestFormatBoundsIntegerDigits checks that a custom locale whose units leave a gap, or start far above 10^3, writes scientific output rather than a string of zeros.
func TestFormatBoundsIntegerDigits(t *testing.T) {
	gapped := &idle.Locale{Version: 1, Name: "x-gap", Decimal: ".", Group: ",", Exponent: "e", Units: []idle.Unit{{Exp: 3, Suffix: "K"}, {Exp: 1000000, Suffix: "X"}}}
	high := &idle.Locale{Version: 1, Name: "x-high", Decimal: ".", Group: ",", Exponent: "e", Units: []idle.Unit{{Exp: 1000000, Suffix: "X"}}}
	for _, l := range []*idle.Locale{gapped, high} {
		if err := l.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	checkFormat(t, idle.Format{Locale: gapped}, []formatCase{
		{idle.New(1500), "1.5K"},
		{idle.NewExp(1, 23), "100000000000000000000K"},
		{idle.NewExp(1, 24), "1e24"},
		{idle.NewExp(1, 999999), "1e999999"},
		{idle.NewExp(1.5, 1000000), "1.5X"},
		{idle.NewExp(1, 1000020), "100000000000000000000X"},
		{idle.NewExp(1, 1000021), "1e1000021"},
	})
	checkFormat(t, idle.Format{Locale: high}, []formatCase{
		{idle.NewExp(1, 20), "100,000,000,000,000,000,000"},
		{idle.NewExp(1, 21), "1e21"},
		{idle.NewExp(1, 999999), "1e999999"},
		{idle.NewExp(2, 1000000), "2X"},
	})
	n := idle.NewExp(1, 999999)
	buf := make([]byte, 0, 64)
	if allocs := testing.AllocsPerRun(100, func() { buf = idle.Format{Locale: gapped}.Append(buf[:0], n) }); allocs != 0 {
		t.Errorf("gapped Append allocates %v times", allocs)
	}
}

// TestDocumentedTable checks the notation table in docs/en/idle.md and docs/zh/idle.md.
func TestDocumentedTable(t *testing.T) {
	formats := []idle.Format{
		{},
		{Locale: idle.ChineseSimplified()},
		{Notation: idle.Scientific},
		{Notation: idle.Engineering},
		{Notation: idle.Letters},
		{Notation: idle.Full},
	}
	rows := []struct {
		n    idle.Number
		want [6]string
	}{
		{idle.New(999), [6]string{"999", "999", "999", "999", "999", "999"}},
		{idle.New(12345), [6]string{"12.3K", "1.23万", "1.23e4", "12.3e3", "12.3K", "12,345"}},
		{idle.New(123456789), [6]string{"123M", "1.23亿", "1.23e8", "123e6", "123M", "123,456,789"}},
		{idle.New(4.56e15), [6]string{"4.56Qa", "4560兆", "4.56e15", "4.56e15", "4.56aa", "4,560,000,000,000,000"}},
		{idle.NewExp(1.23, 45), [6]string{"1.23Qad", "12.3载", "1.23e45", "1.23e45", "1.23ak", "1.23e45"}},
		{idle.NewExp(1, 70), [6]string{"1e70", "1e70", "1e70", "10e69", "10as", "1e70"}},
	}
	for _, r := range rows {
		for i, f := range formats {
			if got := f.Format(r.n); got != r.want[i] {
				t.Errorf("%v column %d: %q, want %q", r.n, i, got, r.want[i])
			}
		}
	}
}
