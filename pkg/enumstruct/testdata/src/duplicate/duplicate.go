package duplicate

//enumstruct:decl
type Union struct { // want Union:`\[A B\]`
	A *int
	B *string
}

func check(u Union) {
	switch {
	case u.A != nil:
	case u.A != nil: // want `duplicate nil-check case for field A`
	case u.B != nil:
	}
}
