// Command enumstruct runs the enumstruct linter as a standalone binary.
//
// Usage:
//
//	enumstruct ./...
//	enumstruct ./pkg/graphql/...
package main

import (
	"github.com/TheFellow/enumstruct/pkg/enumstruct"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(enumstruct.Analyzer)
}
