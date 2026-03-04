// Package enumstruct provides a go/analysis linter that enforces exhaustive
// nil-check switches on pointer-union structs (enum structs).
//
// A pointer-union struct is a struct where every field is a pointer type and
// exactly one is expected to be non-nil at runtime — a discriminated union.
//
// # Declaring enum structs
//
// Annotation (for owned code):
//
//	//enumstruct:decl
//	type MyUnion struct { A *TypeA; B *TypeB }
//
// Config (for generated/imported code, e.g. gqlgen models):
//
//	# .enumstruct.yml
//	types:
//	  - "github.com/org/pkg/model.MyInput"
//
// # Directives
//
//   - //enumstruct:decl                 — marks struct as a pointer-union
//   - //enumstruct:ignore               — suppresses exhaustiveness for one switch
//   - //enumstruct:ignore-field <Name>  — excludes a field from exhaustiveness
package enumstruct

import (
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer is the go/analysis entry point for enumstruct.
var Analyzer = &analysis.Analyzer{
	Name:      "enumstruct",
	Doc:       "checks exhaustiveness of nil-check switches on pointer-union structs",
	Run:       run,
	Requires:  []*analysis.Analyzer{inspect.Analyzer},
	FactTypes: []analysis.Fact{(*isEnumStruct)(nil)},
}

type enumInfo struct {
	fields         []*types.Var
	excludedFields []string
}

var configCache sync.Map // map[string]cachedConfig

var configPathCache sync.Map // map[string]string

type cachedConfig struct {
	cfg Config
	err error
}

func run(pass *analysis.Pass) (interface{}, error) {
	ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	cfg, err := loadConfigCached(projectDir(pass))
	if err != nil {
		return nil, err
	}

	enumTypes := map[*types.Named]enumInfo{}
	scanAnnotatedEnumStructs(pass, enumTypes)
	registerConfigEnumStructs(pass, cfg, enumTypes)
	ignoreSwitchLines := collectIgnoreNextSwitchLines(pass)

	nodeFilter := []ast.Node{(*ast.SwitchStmt)(nil)}
	ins.Preorder(nodeFilter, func(n ast.Node) {
		sw := n.(*ast.SwitchStmt)
		if sw.Tag != nil {
			return
		}
		if !shouldCheckGenerated(cfg) {
			if file := fileForPos(pass, sw.Pos()); file != nil && ast.IsGenerated(file) {
				return
			}
		}

		swPos := pass.Fset.PositionFor(sw.Switch, false)
		if ignoreSwitchLines[swPos.Filename][swPos.Line] {
			return
		}

		var recvNamed *types.Named
		hasReceiverMismatch := false
		hasDefault := false

		for _, stmt := range sw.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			if clause.List == nil {
				hasDefault = true
				continue
			}
			for _, expr := range clause.List {
				_, named := fieldCheckFromExpr(pass, expr)
				if named == nil {
					continue
				}
				if recvNamed == nil {
					recvNamed = named
					continue
				}
				if recvNamed != named {
					hasReceiverMismatch = true
					break
				}
			}
			if hasReceiverMismatch {
				break
			}
		}
		if recvNamed == nil || hasReceiverMismatch {
			return
		}

		info, ok := enumTypes[recvNamed]
		if !ok {
			var fact isEnumStruct
			if recvNamed.Obj() == nil || !pass.ImportObjectFact(recvNamed.Obj(), &fact) {
				return
			}
			info = enumInfoFromFact(recvNamed, fact)
			enumTypes[recvNamed] = info
		}

		expected := map[*types.Var]bool{}
		for _, f := range info.fields {
			expected[f] = true
		}
		if len(expected) == 0 {
			return
		}

		seen := map[*types.Var]bool{}
		for _, stmt := range sw.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok || clause.List == nil {
				continue
			}
			for _, expr := range clause.List {
				field, named := fieldCheckFromExpr(pass, expr)
				if field == nil || named == nil || named != recvNamed {
					continue
				}
				if !expected[field] {
					continue
				}
				if seen[field] {
					pass.Reportf(expr.Pos(), "duplicate nil-check case for field %s", field.Name())
					continue
				}
				seen[field] = true
			}
		}

		if strings.EqualFold(cfg.DefaultMode, "lenient") && hasDefault {
			return
		}

		var missing []string
		for _, field := range info.fields {
			if !seen[field] {
				missing = append(missing, field.Name())
			}
		}
		if len(missing) == 0 {
			return
		}

		typeName := recvNamed.Obj().Name()
		pkgPath := ""
		if recvNamed.Obj().Pkg() != nil {
			pkgPath = recvNamed.Obj().Pkg().Path()
		}
		if pkgPath != "" {
			typeName = pkgPath + "." + typeName
		}
		pass.Reportf(
			sw.Switch,
			"exhaustiveness check failed for '%s': missing cases: %s\nAdd cases or suppress with //enumstruct:ignore",
			typeName,
			strings.Join(missing, ", "),
		)
	})

	return nil, nil
}

func scanAnnotatedEnumStructs(pass *analysis.Pass, enumTypes map[*types.Named]enumInfo) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if !hasDeclDirective(pass, typeSpec, genDecl) {
					continue
				}

				obj, ok := pass.TypesInfo.Defs[typeSpec.Name].(*types.TypeName)
				if !ok {
					continue
				}
				named, ok := obj.Type().(*types.Named)
				if !ok {
					continue
				}
				under, ok := named.Underlying().(*types.Struct)
				if !ok {
					continue
				}

				excluded := collectIgnoreFieldNames(typeSpec, genDecl)
				excludedSet := make(map[string]bool, len(excluded))
				for _, name := range excluded {
					excludedSet[name] = true
				}

				var allFieldNames []string
				var fields []*types.Var
				for i := range under.NumFields() {
					field := under.Field(i)
					if !isPointer(field.Type()) {
						if !excludedSet[field.Name()] {
							pass.Reportf(
								typeSpec.Pos(),
								"enumstruct: field %q of %s is not a pointer type; all fields of a pointer-union struct should be pointers or excluded via //enumstruct:ignore-field",
								field.Name(),
								typeSpec.Name.Name,
							)
						}
						continue
					}
					allFieldNames = append(allFieldNames, field.Name())
					if excludedSet[field.Name()] {
						continue
					}
					fields = append(fields, field)
				}

				enumTypes[named] = enumInfo{
					fields:         fields,
					excludedFields: slices.Clone(excluded),
				}
				pass.ExportObjectFact(obj, &isEnumStruct{
					Fields:         allFieldNames,
					ExcludedFields: slices.Clone(excluded),
				})
			}
		}
	}
}

func registerConfigEnumStructs(pass *analysis.Pass, cfg Config, enumTypes map[*types.Named]enumInfo) {
	importsByPath := make(map[string]*types.Package, len(pass.Pkg.Imports()))
	for _, pkg := range pass.Pkg.Imports() {
		importsByPath[pkg.Path()] = pkg
	}

	for _, fullType := range cfg.Types {
		idx := strings.LastIndex(fullType, ".")
		if idx <= 0 || idx >= len(fullType)-1 {
			continue
		}
		importPath := fullType[:idx]
		typeName := fullType[idx+1:]

		pkg, ok := importsByPath[importPath]
		if !ok {
			reportConfigTypeNotFound(pass, fullType)
			continue
		}
		sym := pkg.Scope().Lookup(typeName)
		if sym == nil {
			reportConfigTypeNotFound(pass, fullType)
			continue
		}
		obj, ok := sym.(*types.TypeName)
		if !ok {
			reportConfigTypeNotFound(pass, fullType)
			continue
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			continue
		}
		under, ok := named.Underlying().(*types.Struct)
		if !ok {
			continue
		}

		excluded := slices.Clone(cfg.ExcludeFields[fullType])
		excludedSet := make(map[string]bool, len(excluded))
		for _, name := range excluded {
			excludedSet[name] = true
		}

		var fields []*types.Var
		for i := range under.NumFields() {
			field := under.Field(i)
			if !isPointer(field.Type()) || excludedSet[field.Name()] {
				continue
			}
			fields = append(fields, field)
		}
		enumTypes[named] = enumInfo{
			fields:         fields,
			excludedFields: excluded,
		}
	}
}

// enumInfoFromFact reconstructs enumInfo from a cross-package fact.
// The *types.Var pointers returned by under.Field(i) have identity-equal
// objects to those returned by pass.TypesInfo.Selections because the
// go/types importer shares object identity within a single analysis driver run.
// Facts store field names as strings (gob serialization cannot handle *types.Var
// pointers), but reconstruction via the named type's struct is correct.
func enumInfoFromFact(named *types.Named, fact isEnumStruct) enumInfo {
	under, ok := named.Underlying().(*types.Struct)
	if !ok {
		return enumInfo{}
	}

	included := make(map[string]bool, len(fact.Fields))
	for _, name := range fact.Fields {
		included[name] = true
	}
	excluded := make(map[string]bool, len(fact.ExcludedFields))
	for _, name := range fact.ExcludedFields {
		excluded[name] = true
	}

	var fields []*types.Var
	for i := range under.NumFields() {
		field := under.Field(i)
		if !isPointer(field.Type()) {
			continue
		}
		if !included[field.Name()] || excluded[field.Name()] {
			continue
		}
		fields = append(fields, field)
	}
	return enumInfo{
		fields:         fields,
		excludedFields: slices.Clone(fact.ExcludedFields),
	}
}

func fieldCheckFromExpr(pass *analysis.Pass, expr ast.Expr) (*types.Var, *types.Named) {
	bin, ok := unwrapParens(expr).(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return nil, nil
	}

	left := unwrapParens(bin.X)
	right := unwrapParens(bin.Y)

	var sel *ast.SelectorExpr
	switch {
	case isNilIdent(left):
		sel, ok = right.(*ast.SelectorExpr)
	case isNilIdent(right):
		sel, ok = left.(*ast.SelectorExpr)
	default:
		return nil, nil
	}
	if !ok {
		return nil, nil
	}

	selection := pass.TypesInfo.Selections[sel]
	if selection == nil {
		return nil, nil
	}
	field, ok := selection.Obj().(*types.Var)
	if !ok {
		return nil, nil
	}
	recv := derefNamed(pass.TypesInfo.TypeOf(sel.X))
	if recv == nil {
		return nil, nil
	}
	return field, recv
}

func projectDir(pass *analysis.Pass) string {
	if len(pass.Files) == 0 {
		return "."
	}
	file := pass.Fset.File(pass.Files[0].Pos())
	if file == nil {
		return "."
	}
	return filepath.Dir(file.Name())
}

func collectIgnoreNextSwitchLines(pass *analysis.Pass) map[string]map[int]bool {
	lines := map[string]map[int]bool{}
	for _, file := range pass.Files {
		fpos := pass.Fset.File(file.Pos())
		if fpos == nil {
			continue
		}
		filename := fpos.Name()
		for _, cg := range file.Comments {
			for _, c := range cg.List {
				if strings.TrimSpace(c.Text) != "//enumstruct:ignore" {
					continue
				}
				line := pass.Fset.PositionFor(c.Slash, false).Line
				if lines[filename] == nil {
					lines[filename] = map[int]bool{}
				}
				lines[filename][line+1] = true
				lines[filename][line+2] = true
				lines[filename][line+3] = true
			}
		}
	}
	return lines
}

func fileForPos(pass *analysis.Pass, pos token.Pos) *ast.File {
	for _, file := range pass.Files {
		if file.Pos() <= pos && pos <= file.End() {
			return file
		}
	}
	return nil
}

func hasDeclDirective(pass *analysis.Pass, typeSpec *ast.TypeSpec, genDecl *ast.GenDecl) bool {
	line := pass.Fset.PositionFor(typeSpec.Pos(), false).Line
	check := func(cg *ast.CommentGroup) bool {
		if cg == nil {
			return false
		}
		endLine := pass.Fset.PositionFor(cg.End(), false).Line
		if endLine+1 != line {
			return false
		}
		for _, c := range cg.List {
			if strings.TrimSpace(c.Text) == "//enumstruct:decl" {
				return true
			}
		}
		return false
	}
	if check(typeSpec.Doc) {
		return true
	}
	return check(genDecl.Doc)
}

func collectIgnoreFieldNames(typeSpec *ast.TypeSpec, genDecl *ast.GenDecl) []string {
	out := map[string]bool{}
	parse := func(cg *ast.CommentGroup) {
		if cg == nil {
			return
		}
		for _, c := range cg.List {
			txt := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			if !strings.HasPrefix(txt, "enumstruct:ignore-field ") {
				continue
			}
			name := strings.TrimSpace(strings.TrimPrefix(txt, "enumstruct:ignore-field "))
			if name != "" {
				out[name] = true
			}
		}
	}
	parse(typeSpec.Doc)
	parse(genDecl.Doc)

	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func shouldCheckGenerated(cfg Config) bool {
	if cfg.CheckGenerated == nil {
		return true
	}
	return *cfg.CheckGenerated
}

func loadConfigCached(startDir string) (Config, error) {
	key, configPath, err := resolveConfigCacheKey(startDir)
	if err != nil {
		return Config{}, err
	}
	if cached, ok := configCache.Load(key); ok {
		c := cached.(cachedConfig)
		return c.cfg, c.err
	}

	var cfg Config
	if configPath == "" {
		cfg = defaultConfig()
	} else {
		cfg, err = loadConfigFromPath(configPath)
	}
	c := cachedConfig{cfg: cfg, err: err}
	configCache.Store(key, c)
	return c.cfg, c.err
}

func resolveConfigCacheKey(startDir string) (key string, configPath string, err error) {
	if v, ok := configPathCache.Load(startDir); ok {
		resolved := v.(string)
		if resolved == "" {
			return startDir, "", nil
		}
		return resolved, resolved, nil
	}

	current := startDir
	var visited []string
	for {
		if v, ok := configPathCache.Load(current); ok {
			resolved := v.(string)
			if resolved == "" {
				for _, dir := range visited {
					configPathCache.Store(dir, "")
				}
				return startDir, "", nil
			}
			for _, dir := range visited {
				configPathCache.Store(dir, resolved)
			}
			return resolved, resolved, nil
		}

		configPathCandidate := filepath.Join(current, ".enumstruct.yml")
		_, statErr := os.Stat(configPathCandidate)
		if statErr == nil {
			configPathCache.Store(current, configPathCandidate)
			for _, dir := range visited {
				configPathCache.Store(dir, configPathCandidate)
			}
			return configPathCandidate, configPathCandidate, nil
		}
		if !os.IsNotExist(statErr) {
			return "", "", statErr
		}

		visited = append(visited, current)
		parent := filepath.Dir(current)
		if parent == current {
			for _, dir := range visited {
				configPathCache.Store(dir, "")
			}
			return startDir, "", nil
		}
		current = parent
	}
}

func reportConfigTypeNotFound(pass *analysis.Pass, fullType string) {
	if len(pass.Files) > 0 {
		pass.Reportf(pass.Files[0].Pos(), "enumstruct: config type %q not found; check the import path and type name", fullType)
	}
}

func isNilIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

func unwrapParens(e ast.Expr) ast.Expr {
	for {
		paren, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = paren.X
	}
}

func derefNamed(t types.Type) *types.Named {
	switch v := t.(type) {
	case *types.Named:
		return v
	case *types.Pointer:
		if named, ok := v.Elem().(*types.Named); ok {
			return named
		}
	}
	return nil
}

func isPointer(t types.Type) bool {
	_, ok := t.(*types.Pointer)
	return ok
}
