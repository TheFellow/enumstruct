package strict_default

//enumstruct:decl
type Union struct { // want Union:`\[A B\]`
	A *int
	B *string
}

func check(u Union) {
	switch { // want `missing cases: B`
	case u.A != nil:
	default:
	}
}
