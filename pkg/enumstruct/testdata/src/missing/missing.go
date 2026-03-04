package missing

//enumstruct:decl
type Union struct { // want Union:`\[A B C\]`
	A *int
	B *string
	C *bool
}

func check(u Union) {
	switch { // want `missing cases: C`
	case u.A != nil:
	case u.B != nil:
	}
}
