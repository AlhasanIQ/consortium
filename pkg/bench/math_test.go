package bench

import "testing"

func TestParseMathAnswer(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   string
		wantOK bool
	}{
		{
			name:   "final answer line",
			raw:    "Some reasoning.\nFinal answer: \\frac{14}{3}\n",
			want:   `\frac{14}{3}`,
			wantOK: true,
		},
		{
			name:   "boxed expression",
			raw:    "Therefore the result is \\boxed{\\frac{3}{2}}.",
			want:   `\frac{3}{2}`,
			wantOK: true,
		},
		{
			name:   "dfrac normalized",
			raw:    "Answer: \\dfrac{3}{2}",
			want:   `\frac{3}{2}`,
			wantOK: true,
		},
		{
			name:   "short single line fallback",
			raw:    "14/3",
			want:   "14/3",
			wantOK: true,
		},
		{
			name:   "long unstructured output rejected",
			raw:    "This response has no explicit final answer marker and keeps going with prose.\nLine 2\nLine 3\nLine 4 with a lot of text that should not be treated as the canonical answer",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseMathAnswer(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("ParseMathAnswer ok=%v, want %v (got=%q)", ok, tt.wantOK, got)
			}
			if tt.wantOK && got != tt.want {
				t.Fatalf("ParseMathAnswer=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestMathAnswersEquivalent(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{
			name: "exact match",
			a:    "42",
			b:    "42",
			want: true,
		},
		{
			name: "fraction and decimal",
			a:    "1/2",
			b:    "0.5",
			want: true,
		},
		{
			name: "latex fraction",
			a:    `\frac{14}{3}`,
			b:    "14/3",
			want: true,
		},
		{
			name: "pi fraction forms",
			a:    `-\frac{\pi}{6}`,
			b:    "-pi/6",
			want: true,
		},
		{
			name: "assignment versus value",
			a:    "x = 5",
			b:    "5",
			want: true,
		},
		{
			name: "tuple normalized spacing",
			a:    "(1, -2)",
			b:    "1,-2",
			want: true,
		},
		{
			name: "uppercase latex and unicode pi tuple",
			a:    "(3,\u03c0/2)",
			b:    `\LEFT( 3, \FRAC{\PI}{2} \RIGHT)`,
			want: true,
		},
		{
			name: "units are ignored for numeric answers",
			a:    "72 degrees",
			b:    "72",
			want: true,
		},
		{
			name: "thousands separators in numbers",
			a:    "1,000",
			b:    "1000",
			want: true,
		},
		{
			name: "different values",
			a:    "7",
			b:    "8",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MathAnswersEquivalent(tt.a, tt.b)
			if got != tt.want {
				t.Fatalf("MathAnswersEquivalent(%q, %q)=%v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestNormalizeMathAnswer(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"nested boxed", `\boxed{\boxed{42}}`, "42"},
		{"unicode minus", "−5", "-5"},
		{"left right stripped", `\left(\frac{1}{2}\right)`, `(\frac{1}{2})`},
		{"dollar sign wrapper", "$3x + 1$", "3x + 1"},
		{"backtick wrapper", "`42`", "42"},
		{"trailing punctuation", "42.", "42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeMathAnswer(tt.raw)
			if got != tt.want {
				t.Fatalf("NormalizeMathAnswer(%q)=%q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseMathAnswerJSON(t *testing.T) {
	got, ok := ParseMathAnswer(`{"answer": "42"}`)
	if !ok {
		t.Fatal("expected JSON answer to parse")
	}
	if got != "42" {
		t.Fatalf("ParseMathAnswer JSON=%q, want %q", got, "42")
	}
}

func TestParseMathAnswerMultipleBoxed(t *testing.T) {
	// Should extract the last \boxed
	raw := `First attempt: \boxed{3}. Wait, recalculating... \boxed{7}`
	got, ok := ParseMathAnswer(raw)
	if !ok {
		t.Fatal("expected multiple boxed to parse")
	}
	if got != "7" {
		t.Fatalf("ParseMathAnswer last boxed=%q, want %q", got, "7")
	}
}

func TestParseNumericValueEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want float64
		ok   bool
	}{
		{"percentage", "50%", 0.5, true},
		{"sqrt expression", `\sqrt{4}`, 2.0, true},
		{"comma thousands", "1,000", 1000.0, true},
		// Note: \frac{}{} regex requires non-brace content, so nested \frac
		// doesn't parse via latexFracRegex. It falls through to the "/" splitter.
		{"simple slash frac", "1/6", 1.0 / 6.0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseNumericValue(tt.raw, 0)
			if ok != tt.ok {
				t.Fatalf("parseNumericValue ok=%v, want %v", ok, tt.ok)
			}
			if tt.ok && !almostEqual(got, tt.want) {
				t.Fatalf("parseNumericValue=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseNumericValueDepthLimit(t *testing.T) {
	_, ok := parseNumericValue(`\frac{1}{2}`, maxComparisonDepth+1)
	if ok {
		t.Fatal("expected depth limit to prevent parsing")
	}
}

func TestMathAnswersEquivalentDepthLimit(t *testing.T) {
	// Should return false (not panic) when recursion limit is exceeded.
	// mathAnswersEquivalent at maxComparisonDepth+1 should immediately return false.
	got := mathAnswersEquivalent("42", "42", maxComparisonDepth+1)
	if got {
		t.Fatal("expected depth limit to return false")
	}
}

func TestIsCorrectPredictionMath500(t *testing.T) {
	item := DatasetItem{
		Benchmark:   BenchmarkMath500,
		AnswerLabel: `\frac{14}{3}`,
		GoldAnswer:  `\frac{14}{3}`,
	}
	if !IsCorrectPrediction(item, "14/3", true) {
		t.Fatal("expected equivalent math answer to be correct")
	}
	if IsCorrectPrediction(item, "3/14", true) {
		t.Fatal("expected non-equivalent math answer to be incorrect")
	}
	if IsCorrectPrediction(item, "14/3", false) {
		t.Fatal("parse failure should never be marked correct")
	}
}

// --- Gap fix regression tests (from math-500 failure analysis) ---

func TestMathNormalizerGap1_ShortFormFrac(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"frac digit braced denom", `\frac9{19}`, "9/19"},
		{"FRAC uppercase digit braced", `\FRAC9{19}`, "9/19"},
		{"frac two digits no braces", `\frac14`, "1/4"},
		{"frac space two digits", `\frac 34`, "3/4"},
		{"frac two digits 65", `\frac65`, "6/5"},
		{"frac space two digits 59", `\frac 59`, "5/9"},
		{"frac two digits 43", `\frac43`, "4/3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !MathAnswersEquivalent(tt.a, tt.b) {
				t.Fatalf("MathAnswersEquivalent(%q, %q) = false, want true", tt.a, tt.b)
			}
		})
	}
}

func TestMathNormalizerGap2_InlineTextMbox(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"mbox inches squared", `864 \mbox{ inches}^2`, "864"},
		{"text degrees", `\frac{270}{7}\text{ degrees}`, `\frac{270}{7}`},
		{"text cents", `5.4 \text{ cents}`, "5.4"},
		{"MBOX uppercase", `100 \MBOX{ METERS}`, "100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !MathAnswersEquivalent(tt.a, tt.b) {
				t.Fatalf("MathAnswersEquivalent(%q, %q) = false, want true", tt.a, tt.b)
			}
		})
	}
}

func TestMathNormalizerGap3_CurrencySymbol(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"escaped dollar", `\$36`, "36"},
		{"escaped dollar decimal", `\$18.90`, "18.90"},
		{"escaped dollar thousands", `\$32,348`, "32348"},
		{"bare dollar sign", "$42", "42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !MathAnswersEquivalent(tt.a, tt.b) {
				t.Fatalf("MathAnswersEquivalent(%q, %q) = false, want true", tt.a, tt.b)
			}
		})
	}
}

func TestMathNormalizerGap4_ThousandsSeparatorSpacing(t *testing.T) {
	// After \! removal, spaces remain around commas in thousands separators
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"spaced thousands from \\! removal", `11,\! 111,\! 111,\! 100`, "11111111100"},
		{"single spaced group", `1, 000`, "1000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !MathAnswersEquivalent(tt.a, tt.b) {
				t.Fatalf("MathAnswersEquivalent(%q, %q) = false, want true", tt.a, tt.b)
			}
		})
	}
}

func TestMathNormalizerGap5_SetOrderIndependence(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"two elements swapped", "1,-2", "-2, 1"},
		{"three elements reordered", "1,2,3", "3,1,2"},
		{"parenthesized", "(1, -2)", "(-2, 1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !MathAnswersEquivalent(tt.a, tt.b) {
				t.Fatalf("MathAnswersEquivalent(%q, %q) = false, want true", tt.a, tt.b)
			}
		})
	}

	// Negative: different elements should not match
	if MathAnswersEquivalent("1,2", "1,3") {
		t.Fatal("different set elements should not match")
	}
}

func TestMathNormalizerGap6_PlusMinusExpansion(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"pm sqrt", `1 \pm \sqrt{19}`, `1 + \sqrt{19}, 1 - \sqrt{19}`},
		{"pm reversed order", `3 \pm 2\sqrt{2}`, `3 - 2\sqrt{2}, 3 + 2\sqrt{2}`},
		{"unicode pm", "5 \u00b1 x", "5 + x, 5 - x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !MathAnswersEquivalent(tt.a, tt.b) {
				t.Fatalf("MathAnswersEquivalent(%q, %q) = false, want true", tt.a, tt.b)
			}
		})
	}
}

func TestMathNormalizerGap7_MarkdownBold(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"bold wrapped answer", "**47**", "47"},
		{"bold final answer prefix", "**Final answer: 47**", "47"},
		{"bold with frac", `**Final answer: \(\frac{3}{2}\)**`, `\frac{3}{2}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !MathAnswersEquivalent(tt.a, tt.b) {
				t.Fatalf("MathAnswersEquivalent(%q, %q) = false, want true", tt.a, tt.b)
			}
		})
	}

	// Also test ParseMathAnswer with bold in multi-line output
	raw := "Step 1: blah\nStep 2: blah\n**Final answer: 42**"
	got, ok := ParseMathAnswer(raw)
	if !ok {
		t.Fatal("ParseMathAnswer should handle **Final answer:**")
	}
	if got != "42" {
		t.Fatalf("ParseMathAnswer bold=%q, want %q", got, "42")
	}
}

func TestMathNormalizerGap8_SubscriptBraces(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"subscript braces vs bare", `4210_{5}`, `4210_5`},
		{"multi-char subscript unchanged", `x_{12}`, `x_{12}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !MathAnswersEquivalent(tt.a, tt.b) {
				t.Fatalf("MathAnswersEquivalent(%q, %q) = false, want true", tt.a, tt.b)
			}
		})
	}
}

func TestExpandPlusMinus(t *testing.T) {
	// Should not match \pmatrix or other commands containing \pm
	expanded, ok := expandPlusMinus(`\pmatrix{1 & 2}`)
	if ok {
		t.Fatalf("expandPlusMinus should not match \\pmatrix, got %v", expanded)
	}
}
