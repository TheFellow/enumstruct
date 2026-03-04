package non_pointer_field

//enumstruct:decl
type Bad struct { // want "field \"B\" of Bad is not a pointer type" Bad:`\[A C\]`
	A *int
	B string
	C *bool
}
