package alias

//enumstruct:decl
type Union struct { // want Union:`\[A B C\]`
	A *int
	B *string
	C *bool
}

// ValueAlias is an alias for the union type itself.
type ValueAlias = Union

// PointerAlias is an alias for a pointer to the union type.
type PointerAlias = *Union

func checkValueAlias(u ValueAlias) {
	switch { // want `missing cases: C`
	case u.A != nil:
	case u.B != nil:
	}
}

func checkPointerAlias(u PointerAlias) {
	switch { // want `missing cases: C`
	case u.A != nil:
	case u.B != nil:
	}
}

func checkAliasExhaustive(u ValueAlias) {
	switch {
	case u.A != nil:
	case u.B != nil:
	case u.C != nil:
	}
}

// IntPtr is an alias for a pointer type, used as a union field type.
type IntPtr = *int

//enumstruct:decl
type AliasFieldUnion struct { // want AliasFieldUnion:`\[A B\]`
	A IntPtr
	B *string
}

func checkAliasField(u AliasFieldUnion) {
	switch { // want `missing cases: B`
	case u.A != nil:
	}
}
