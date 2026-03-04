package ignore_next_switch

//enumstruct:decl
type Union struct { // want Union:`\[A B\]`
	A *int
	B *string
}

func check(u Union) {
	//enumstruct:ignore
	switch {
	case u.A != nil:
	}
}
