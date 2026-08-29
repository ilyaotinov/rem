package main

import (
	"errors"
	"testing"
)

func TestParsePeriodFromStr(t *testing.T) {
	type expected struct {
		period Period
		err    error
	}

	tests := []struct {
		name     string
		input    string
		expected expected
	}{
		{
			name:  "5d",
			input: "5d",
			expected: expected{
				period: Period{
					Length: 5,
					Kind:   PeriodKindDay,
				},
			},
		},
		{
			name:  "invalid perid",
			input: "d5",
			expected: expected{
				err: &InvalidPeriodError{UnparsedPeriod: "d5"},
			},
		},
		{
			name:  "invalid modifier",
			input: "5test",
			expected: expected{
				err: &UnknownPeriodModifierError{
					UnparsedModifier: "test",
					PeriodLength:     5,
				},
			},
		},
		{
			name:  "16w",
			input: "16w",
			expected: expected{
				period: Period{
					Length: 16,
					Kind:   PeriodKindWeek,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePeriodFromStr(tt.input)
			if tt.expected.err != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.expected.err)
				}

				if !errors.Is(err, tt.expected.err) {
					t.Fatalf("unexpected error. expected: `%v`, got: `%v`",
						tt.expected.err, err)
				}

				return
			}

			assertNoError(t, err)

			if tt.expected.period.Length != got.Length {
				t.Fatalf("unexpected period length. expected: %d, got: %d",
					tt.expected.period.Length, got.Length)
			}

			if tt.expected.period.Kind != got.Kind {
				t.Fatalf("unexpected period kind. expected: %v, got: %v",
					tt.expected.period.Kind, got.Kind)
			}
		})
	}
}
