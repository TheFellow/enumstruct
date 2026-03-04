package enumstruct

type isEnumStruct struct {
	Fields         []string
	ExcludedFields []string
}

func (*isEnumStruct) AFact() {}
