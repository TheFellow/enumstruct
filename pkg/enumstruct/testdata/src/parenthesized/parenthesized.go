package parenthesized

//enumstruct:decl
type Union struct { // want Union:`\[A B\]`
	A *int
	B *string
}

func check(u Union) {
	switch {
	case (u.A != nil):
	case (u.B != nil):
	}
}
