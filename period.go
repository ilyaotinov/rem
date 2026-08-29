package main

import (
	"database/sql"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
)

type PeriodKind int

const (
	PeriodKindNone PeriodKind = iota
	PeriodKindDay
	PeriodKindWeek
	PeriodKindMonth
	PeriodKindYears
)

type Period struct {
	Kind   PeriodKind
	Length int
}

func (p Period) AsSQLDatetimeModifier() sql.NullString {
	switch p.Kind {
	case PeriodKindNone:
		return sql.NullString{Valid: false}
	case PeriodKindDay:
		return sql.NullString{String: fmt.Sprintf("%d days", p.Length), Valid: true}
	case PeriodKindWeek:
		return sql.NullString{String: fmt.Sprintf("%d days", p.Length*7), Valid: true}
	case PeriodKindMonth:
		return sql.NullString{String: fmt.Sprintf("%d months", p.Length), Valid: true}
	case PeriodKindYears:
		return sql.NullString{String: fmt.Sprintf("%d years", p.Length), Valid: true}
	default:
		panic("cannot happen")
	}
}

type PeriodModifier struct {
	Modifier string
	Name     string
}

var remPeriodModifiers = map[PeriodKind]PeriodModifier{
	PeriodKindDay: {
		Modifier: "d",
		Name:     "days",
	},
	PeriodKindWeek: {
		Modifier: "w",
		Name:     "weeks",
	},
	PeriodKindMonth: {
		Modifier: "m",
		Name:     "months",
	},
	PeriodKindYears: {
		Modifier: "y",
		Name:     "years",
	},
}

type InvalidPeriodError struct {
	UnparsedPeriod string
}

func (e *InvalidPeriodError) Error() string {
	return fmt.Sprintf("invalid period: %s", e.UnparsedPeriod)
}

func (e *InvalidPeriodError) Is(other error) bool {
	otherErr, ok := other.(*InvalidPeriodError)
	if !ok {
		return false
	}

	return otherErr.UnparsedPeriod == e.UnparsedPeriod
}

func (e *InvalidPeriodError) ExplainUsage() string {
	b := strings.Builder{}
	fmt.Fprintf(&b, "Invalid period `%s`. Expected something like\n",
		e.UnparsedPeriod)
	for _, m := range remPeriodModifiers {
		l := (rand.Int() % 9) + 1
		fmt.Fprintf(&b, "    %d%s - means every %d %s\n", l, m.Modifier, l, m.Name)
	}

	return b.String()
}

type UnknownPeriodModifierError struct {
	UnparsedModifier string
	PeriodLength     int
}

func (e *UnknownPeriodModifierError) Error() string {
	return fmt.Sprintf("unknown period modifier %s", e.UnparsedModifier)
}

func (e *UnknownPeriodModifierError) Is(other error) bool {
	otherErr, ok := other.(*UnknownPeriodModifierError)
	if !ok {
		return false
	}

	return otherErr.PeriodLength == e.PeriodLength &&
		otherErr.UnparsedModifier == e.UnparsedModifier
}

func (e *UnknownPeriodModifierError) ExplainUsage() string {
	b := strings.Builder{}
	fmt.Fprintf(&b, "Unknown period modifier `%s`. Expected mofidiers are\n", e.UnparsedModifier)

	for _, m := range remPeriodModifiers {
		fmt.Fprintf(&b, "    %d%s - means every %d %s\n", e.PeriodLength, m.Modifier,
			e.PeriodLength, m.Modifier)
	}

	return b.String()
}

func ParsePeriodFromStr(unparsedPeriod string) (Period, error) {
	if unparsedPeriod == "none" {
		return Period{}, nil
	}

	var end int
	var ch rune
	for end, ch = range unparsedPeriod {
		if ch < '0' || ch > '9' {
			break
		}
	}

	lenStr, modifierStr := unparsedPeriod[0:end], unparsedPeriod[end:]
	length, err := strconv.Atoi(lenStr)
	if err != nil {
		return Period{}, &InvalidPeriodError{
			UnparsedPeriod: unparsedPeriod,
		}
	}

	if end == 0 {
		return Period{}, &InvalidPeriodError{
			UnparsedPeriod: unparsedPeriod,
		}
	}

	for kind, modifier := range remPeriodModifiers {
		if modifier.Modifier == modifierStr {
			return Period{
				Kind:   kind,
				Length: length,
			}, nil
		}
	}

	return Period{}, &UnknownPeriodModifierError{
		UnparsedModifier: modifierStr,
		PeriodLength:     length,
	}
}
