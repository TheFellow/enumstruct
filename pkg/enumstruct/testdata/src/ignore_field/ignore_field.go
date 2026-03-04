package ignore_field

//enumstruct:decl
//enumstruct:ignore-field Deprecated
type Union struct { // want Union:`\[A Deprecated\]`
	A          *int
	Deprecated *string
}

func check(u Union) {
	switch {
	case u.A != nil:
	}
}
