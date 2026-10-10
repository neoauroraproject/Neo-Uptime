package monitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// evalJSONQuery reads a value out of a JSON document with a small path syntax
// — `data.items[0].status`, `$.status`, `[2].name` — and compares it.
//
// Operators: == != < <= > >= contains notcontains exists. Kuma uses JSONata,
// which is a language; a path and a comparison cover what health endpoints
// actually return and cannot be made to loop or allocate without bound.
func evalJSONQuery(doc []byte, path, op, expected string) (bool, string, error) {
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		return false, "", errors.New("response is not valid JSON")
	}
	got, found, err := walkJSON(v, path)
	if err != nil {
		return false, "", err
	}
	if op == "" {
		op = "=="
	}
	if op == "exists" {
		return found, strconv.FormatBool(found), nil
	}
	if !found {
		return false, "missing", nil
	}
	gs := jsonString(got)
	ok, err := compareValues(gs, op, expected)
	return ok, gs, err
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

// compareValues applies a comparison operator to two values as text, or as
// numbers for the ordering operators.
func compareValues(gs, op, expected string) (bool, error) {
	if op == "" {
		op = "=="
	}
	switch op {
	case "==":
		return gs == expected, nil
	case "!=":
		return gs != expected, nil
	case "contains":
		return strings.Contains(gs, expected), nil
	case "notcontains":
		return !strings.Contains(gs, expected), nil
	case "exists":
		return true, nil
	case "<", "<=", ">", ">=":
		a, err1 := strconv.ParseFloat(strings.TrimSpace(gs), 64)
		b, err2 := strconv.ParseFloat(strings.TrimSpace(expected), 64)
		if err1 != nil || err2 != nil {
			return false, fmt.Errorf("%s needs numbers, got %q and %q", op, gs, expected)
		}
		switch op {
		case "<":
			return a < b, nil
		case "<=":
			return a <= b, nil
		case ">":
			return a > b, nil
		default:
			return a >= b, nil
		}
	}
	return false, fmt.Errorf("unknown operator %q", op)
}

func opLabel(op string) string {
	if op == "" {
		return "=="
	}
	return op
}

func jsonString(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func walkJSON(v any, path string) (any, bool, error) {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return v, true, nil
	}
	cur := v
	i := 0
	for i < len(path) {
		switch path[i] {
		case '.':
			i++
		case '[':
			end := strings.IndexByte(path[i:], ']')
			if end < 0 {
				return nil, false, errors.New("unclosed [ in JSON path")
			}
			key := path[i+1 : i+end]
			i += end + 1
			key = strings.Trim(key, `"'`)
			if n, err := strconv.Atoi(key); err == nil {
				arr, ok := cur.([]any)
				if !ok {
					return nil, false, nil
				}
				if n < 0 {
					n += len(arr)
				}
				if n < 0 || n >= len(arr) {
					return nil, false, nil
				}
				cur = arr[n]
			} else {
				obj, ok := cur.(map[string]any)
				if !ok {
					return nil, false, nil
				}
				if cur, ok = obj[key]; !ok {
					return nil, false, nil
				}
			}
		default:
			j := i
			for j < len(path) && path[j] != '.' && path[j] != '[' {
				j++
			}
			key := path[i:j]
			i = j
			if key == "length" {
				if arr, ok := cur.([]any); ok {
					cur = float64(len(arr))
					continue
				}
			}
			obj, ok := cur.(map[string]any)
			if !ok {
				return nil, false, nil
			}
			if cur, ok = obj[key]; !ok {
				return nil, false, nil
			}
		}
	}
	return cur, true, nil
}
