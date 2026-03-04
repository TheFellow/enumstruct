package reversed_nil

//enumstruct:decl
type Union struct { // want Union:`\[A B\]`
	A *int
	B *string
}

func check(u Union) {
	switch {
	case nil != u.A:
	case nil != u.B:
	}
}
