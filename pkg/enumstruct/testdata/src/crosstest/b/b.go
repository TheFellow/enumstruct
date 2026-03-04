package b

import "crosstest/a"

func check(u a.Union) {
	switch { // want `missing cases: C`
	case u.A != nil:
	case u.B != nil:
	}
}
