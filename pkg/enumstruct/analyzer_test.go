package enumstruct

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	testdata := analysistest.TestData()
	cases := []struct {
		name string
		pkg  string
	}{
		{name: "basic", pkg: "basic"},
		{name: "missing", pkg: "missing"},
		{name: "duplicate", pkg: "duplicate"},
		{name: "ignore-next-switch", pkg: "ignore_next_switch"},
		{name: "ignore-field", pkg: "ignore_field"},
		{name: "lenient-default", pkg: "lenient_default"},
		{name: "strict-default", pkg: "strict_default"},
		{name: "pointer-receiver", pkg: "pointer_receiver"},
		{name: "cross-package", pkg: "crosstest/b"},
		{name: "reversed-nil", pkg: "reversed_nil"},
		{name: "parenthesized", pkg: "parenthesized"},
		{name: "non-pointer-field", pkg: "non_pointer_field"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			analysistest.Run(t, testdata, Analyzer, tc.pkg)
		})
	}
}
