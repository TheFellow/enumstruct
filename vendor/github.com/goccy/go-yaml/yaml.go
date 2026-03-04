package yaml

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Unmarshal implements a minimal YAML parser for project configuration.
// It supports top-level scalar keys, string lists, and map[string][]string.
func Unmarshal(data []byte, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("yaml: non-pointer passed to Unmarshal")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("yaml: expected pointer to struct")
	}

	lines := strings.Split(string(data), "\n")
	var (
		typesItems     []string
		hasTypes       bool
		defaultMode    string
		hasDefaultMode bool
		checkGenerated bool
		hasCheckGen    bool
		excludeFields  = map[string][]string{}
		hasExclude     bool
		section        string
		mapKey         string
	)

	for _, raw := range lines {
		line := stripComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)

		if indent == 0 {
			section = ""
			mapKey = ""
			key, val, ok := splitKV(trimmed)
			if !ok {
				continue
			}
			switch key {
			case "types":
				if val == "" {
					section = "types"
				} else {
					hasTypes = true
					typesItems = parseInlineList(val)
				}
			case "default_mode":
				hasDefaultMode = true
				defaultMode = parseScalar(val)
			case "check_generated":
				b, err := strconv.ParseBool(strings.ToLower(parseScalar(val)))
				if err != nil {
					return fmt.Errorf("yaml: invalid bool for check_generated: %w", err)
				}
				hasCheckGen = true
				checkGenerated = b
			case "exclude_fields":
				hasExclude = true
				section = "exclude_fields"
			}
			continue
		}

		switch section {
		case "types":
			if strings.HasPrefix(trimmed, "- ") {
				hasTypes = true
				typesItems = append(typesItems, parseScalar(strings.TrimPrefix(trimmed, "- ")))
			}
		case "exclude_fields":
			if indent == 2 {
				k, val, ok := splitKV(trimmed)
				if !ok {
					continue
				}
				mapKey = parseScalar(k)
				if _, exists := excludeFields[mapKey]; !exists {
					excludeFields[mapKey] = nil
				}
				if strings.TrimSpace(val) != "" {
					excludeFields[mapKey] = append(excludeFields[mapKey], parseInlineList(val)...)
				}
				continue
			}
			if indent >= 4 && mapKey != "" && strings.HasPrefix(trimmed, "- ") {
				excludeFields[mapKey] = append(excludeFields[mapKey], parseScalar(strings.TrimPrefix(trimmed, "- ")))
			}
		}
	}

	if hasTypes {
		setFieldByYAMLTag(rv, "types", reflect.ValueOf(typesItems))
	}
	if hasDefaultMode {
		setFieldByYAMLTag(rv, "default_mode", reflect.ValueOf(defaultMode))
	}
	if hasCheckGen {
		field := fieldByYAMLTag(rv, "check_generated")
		if field.IsValid() && field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.Bool {
			b := checkGenerated
			field.Set(reflect.ValueOf(&b))
		}
	}
	if hasExclude {
		setFieldByYAMLTag(rv, "exclude_fields", reflect.ValueOf(excludeFields))
	}

	return nil
}

func splitKV(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	return key, value, true
}

func parseInlineList(val string) []string {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
		inside := strings.TrimSpace(val[1 : len(val)-1])
		if inside == "" {
			return nil
		}
		parts := strings.Split(inside, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			item := parseScalar(p)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	}
	return []string{parseScalar(val)}
}

func parseScalar(val string) string {
	val = strings.TrimSpace(val)
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			return val[1 : len(val)-1]
		}
	}
	return val
}

func stripComment(line string) string {
	inSingle := false
	inDouble := false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

func fieldByYAMLTag(rv reflect.Value, tag string) reflect.Value {
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Tag.Get("yaml") == tag {
			return rv.Field(i)
		}
	}
	return reflect.Value{}
}

func setFieldByYAMLTag(rv reflect.Value, tag string, value reflect.Value) {
	field := fieldByYAMLTag(rv, tag)
	if !field.IsValid() || !field.CanSet() {
		return
	}
	if value.Type().AssignableTo(field.Type()) {
		field.Set(value)
		return
	}
	if value.Type().ConvertibleTo(field.Type()) {
		field.Set(value.Convert(field.Type()))
	}
}
