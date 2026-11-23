package idle_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/idle"
)

func ExampleOffline_Earn() {
	offline := idle.Offline{Cap: 8 * time.Hour, Min: time.Minute, Efficiency: 0.5}
	last := time.Date(2026, time.March, 14, 21, 30, 0, 0, time.UTC)
	now := last.Add(26 * time.Hour)

	earned, away := offline.Earn(idle.NewExp(1.2, 9), last, now) // 1.2B gold a second online
	fmt.Println(away.Elapsed, away.Credited, away.Capped)
	fmt.Println(idle.Format{}.Format(earned))
	fmt.Println(idle.Format{Locale: idle.LocaleFor("zh-CN")}.Format(earned))
	// Output:
	// 26h0m0s 8h0m0s true
	// 17.3T
	// 17.3兆
}

func ExampleFormat() {
	n := idle.NewExp(1.2345, 45)
	for _, f := range []idle.Format{
		{},
		{Notation: idle.Scientific},
		{Notation: idle.Engineering},
		{Notation: idle.Letters},
		{Locale: idle.ChineseSimplified()},
		{Digits: 5, Fixed: true},
	} {
		fmt.Println(f.Format(n))
	}
	fmt.Println(idle.Format{Notation: idle.Full}.Format(idle.New(1234567)))
	// Output:
	// 1.23Qad
	// 1.23e45
	// 1.23e45
	// 1.23ak
	// 12.3载
	// 1.2345Qad
	// 1,234,567
}

func ExampleNumber_Pow() {
	base, growth := idle.New(10), idle.New(1.15)
	cost := base.Mul(growth.Pow(250)) // the 251st building
	fmt.Println(idle.Format{Notation: idle.Scientific}.Format(cost))
	// Output:
	// 1.49e16
}
