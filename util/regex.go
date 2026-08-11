package util

import (
	"regexp"
	"regexp/syntax"
	"unicode"
)

// 基于 regexp/syntax 的声明式正则 AST 构造器，用于编译时组合正则表达式。
// 函数式拼接避免字符串拼接转义问题，比 fmt.Sprintf("(%s)|(%s)", ...) 更安全。
//
// Example:
//
//	re := Regex(
//	    StartOfLine,
//	    Either(Then("http"), Then("https")),
//	    Then("://"),
//	    Group("host", Word, Period, Word),
//	)

var (
	regexDigits         = []rune("09")
	regexLowercaseAlpha = []rune("az")
	regexUppercaseAlpha = []rune("AZ")
	regexAlpha          = append(regexUppercaseAlpha, regexLowercaseAlpha...)
	regexAlphanum       = append(regexAlpha, regexDigits...)
	regexWhitespaces    = []rune("  \t\t\n\n")

	// StartOfLine matches the start of a line (^).
	StartOfLine = &syntax.Regexp{Op: syntax.OpBeginLine}
	// EndOfLine matches the end of a line ($).
	EndOfLine = &syntax.Regexp{Op: syntax.OpEndLine}
	// StartOfText matches the start of text (\A).
	StartOfText = &syntax.Regexp{Op: syntax.OpBeginText}
	// EndOfText matches the end of text (\z).
	EndOfText = &syntax.Regexp{Op: syntax.OpEndText}
	// Digit matches a single digit [0-9].
	Digit = &syntax.Regexp{Op: syntax.OpCharClass, Rune: regexDigits}
	// Digits matches one or more digits [0-9]+.
	Digits = &syntax.Regexp{Op: syntax.OpPlus, Sub: []*syntax.Regexp{Digit}}
	// Period matches a literal '.'.
	Period = &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune{'.'}}
	// Lowercase matches a lowercase letter [a-z].
	Lowercase = &syntax.Regexp{Op: syntax.OpCharClass, Rune: regexLowercaseAlpha}
	// Uppercase matches an uppercase letter [A-Z].
	Uppercase = &syntax.Regexp{Op: syntax.OpCharClass, Rune: regexUppercaseAlpha}
	// Alpha matches a letter [A-Za-z].
	Alpha = &syntax.Regexp{Op: syntax.OpCharClass, Rune: regexAlpha}
	// Alphanum matches an alphanumeric character [A-Za-z0-9].
	Alphanum = &syntax.Regexp{Op: syntax.OpCharClass, Rune: regexAlphanum}
	// Anything matches any single character (.).
	Anything = &syntax.Regexp{Op: syntax.OpAnyChar}
	// Word matches one or more alphanumeric characters [A-Za-z0-9]+.
	Word = &syntax.Regexp{Op: syntax.OpPlus, Sub: []*syntax.Regexp{Alphanum}}
	// Newline matches a literal '\n'.
	Newline = &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune("\n")}
	// Whitespace matches one or more whitespace characters.
	Whitespace = &syntax.Regexp{Op: syntax.OpPlus, Sub: []*syntax.Regexp{
		{Op: syntax.OpCharClass, Rune: regexWhitespaces},
	}}
)

// Then matches a literal string.
func RegexThen(match string) *syntax.Regexp {
	return &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune(match)}
}

// RegexRange matches any single rune in the range(s).
func RegexRange(rng ...rune) *syntax.Regexp {
	return &syntax.Regexp{Op: syntax.OpCharClass, Rune: appendClass(nil, rng)}
}

// RegexAnythingBut matches any single character NOT in args.
func RegexAnythingBut(args ...rune) *syntax.Regexp {
	if n := len(args); n%2 == 1 {
		args = append(args, args[n-1])
	}
	return &syntax.Regexp{Op: syntax.OpCharClass, Rune: negateClass(appendClass(nil, args))}
}

// RegexRepeat matches exactly times repetitions of sub.
func RegexRepeat(times int, sub ...*syntax.Regexp) *syntax.Regexp {
	return &syntax.Regexp{Op: syntax.OpRepeat, Min: times, Max: times, Sub: sub}
}

// RegexMaybe optionally matches sub (zero or one).
func RegexMaybe(sub ...any) *syntax.Regexp {
	return &syntax.Regexp{Op: syntax.OpQuest, Sub: []*syntax.Regexp{
		{Op: syntax.OpConcat, Sub: toRegexSyntax(sub...)},
	}}
}

// RegexAtLeastOne matches one or more repetitions of sub.
func RegexAtLeastOne(sub ...*syntax.Regexp) *syntax.Regexp {
	return &syntax.Regexp{Op: syntax.OpPlus, Sub: sub}
}

// RegexMax matches up to times repetitions of sub.
func RegexMax(times int, sub ...*syntax.Regexp) *syntax.Regexp {
	return &syntax.Regexp{Op: syntax.OpRepeat, Max: times, Sub: sub}
}

// RegexMin matches at least times repetitions of sub.
func RegexMin(times int, sub ...*syntax.Regexp) *syntax.Regexp {
	return &syntax.Regexp{Op: syntax.OpRepeat, Min: times, Sub: sub}
}

// RegexEither matches any one of the alternatives.
func RegexEither(sub ...any) *syntax.Regexp {
	return &syntax.Regexp{Op: syntax.OpAlternate, Sub: toRegexSyntax(sub...)}
}

// RegexGroup creates a named capture group. Groups are supported at the
// topmost level of the expression.
func RegexGroup(name string, sub ...*syntax.Regexp) *syntax.Regexp {
	return &syntax.Regexp{Op: syntax.OpCapture, Sub: sub, Name: name}
}

// RegexSequence concatenates subs in order.
func RegexSequence(subs ...*syntax.Regexp) *syntax.Regexp {
	return &syntax.Regexp{Op: syntax.OpConcat, Sub: subs}
}

// RegexBytes compiles the regexp AST and returns a compiled regexp.
// Panics on invalid expression (for init-time use). When you need to
// handle errors at runtime, use CompileRegexBytes instead.
func RegexBytes(subs ...*syntax.Regexp) *regexp.Regexp {
	re, err := CompileRegexBytes(subs...)
	if err != nil {
		panic(err)
	}
	return re
}

// CompileRegexBytes compiles the regexp AST and returns the compiled
// regexp or an error.
func CompileRegexBytes(subs ...*syntax.Regexp) (*regexp.Regexp, error) {
	capIdx := 0
	for _, sub := range subs {
		if sub.Op == syntax.OpCapture {
			sub.Cap = capIdx
			capIdx++
		}
	}
	re := &syntax.Regexp{Op: syntax.OpConcat, Sub: subs}
	re = re.Simplify()
	return regexp.Compile(re.String())
}

// regex helpers below — internal, shared by the AST constructors.

func toRegexSyntax(args ...any) []*syntax.Regexp {
	res := make([]*syntax.Regexp, len(args))
	for i, arg := range args {
		switch a := arg.(type) {
		case string:
			res[i] = RegexThen(a)
		case *syntax.Regexp:
			res[i] = a
		default:
			panic(arg)
		}
	}
	return res
}

func appendRange(r []rune, lo, hi rune) []rune {
	n := len(r)
	for i := 2; i <= 4; i += 2 {
		if n >= i {
			rlo, rhi := r[n-i], r[n-i+1]
			if lo <= rhi+1 && rlo <= hi+1 {
				if lo < rlo {
					r[n-i] = lo
				}
				if hi > rhi {
					r[n-i+1] = hi
				}
				return r
			}
		}
	}
	return append(r, lo, hi)
}

func appendClass(r []rune, x []rune) []rune {
	for i := 0; i < len(x); i += 2 {
		r = appendRange(r, x[i], x[i+1])
	}
	return r
}

func negateClass(r []rune) []rune {
	nextLo := rune(0)
	w := 0
	for i := 0; i < len(r); i += 2 {
		lo, hi := r[i], r[i+1]
		if nextLo <= lo-1 {
			r[w] = nextLo
			r[w+1] = lo - 1
			w += 2
		}
		nextLo = hi + 1
	}
	r = r[:w]
	if nextLo <= unicode.MaxRune {
		r = append(r, nextLo, unicode.MaxRune)
	}
	return r
}

func appendNegatedClass(r []rune, x []rune) []rune {
	nextLo := rune(0)
	for i := 0; i < len(x); i += 2 {
		lo, hi := x[i], x[i+1]
		if nextLo <= lo-1 {
			r = appendRange(r, nextLo, lo-1)
		}
		nextLo = hi + 1
	}
	if nextLo <= unicode.MaxRune {
		r = appendRange(r, nextLo, unicode.MaxRune)
	}
	return r
}
