// Package idle provides the arithmetic idle and incremental games need: a Number that holds values far beyond float64's range, progress earned while the game was closed, and formatting of very large numbers in short (1.23K, 4.5万), letter, scientific (1.23e45), engineering and full forms.
//
// A Number is a plain float64 for magnitudes up to 1e300 and a float64 mantissa with an int64 power of ten beyond, so it reaches 10^MaxExponent instead of float64's 1.8e308 while ordinary values keep exact float64 arithmetic. Its operations are plain IEEE-754 float64 operations with no fused multiply-add, so a given sequence of them gives the same bits on every platform, and it reads and writes itself as text ("1.23e45") for saves and data tables.
//
// Offline credits an absence measured from wall-clock timestamps, with a minimum, a cap, an efficiency and protection against a clock moved backwards. Format renders a Number for a Locale; English and Chinese locales are built in, and a game can describe its own in a versioned YAML file read by ParseLocale.
package idle
