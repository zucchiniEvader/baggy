package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// ValidateArgs checks args against the tool's JSON Schema. Values are first
// coerced the way a model tends to get them wrong (numbers and booleans as
// strings, null for a field it meant to leave out), then validated. The
// returned arguments are what the tool should run with; an error is one
// message the model can act on.
//
// Supported keywords: type, enum, properties, required, additionalProperties
// (false), items, minimum, maximum. Others are ignored, so a schema that uses
// them still validates what it can.
func ValidateArgs(name string, schema, args json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	var s map[string]any
	if len(schema) > 0 {
		dec := json.NewDecoder(bytes.NewReader(schema))
		if err := dec.Decode(&s); err != nil {
			return args, nil // a schema we cannot read is the tool's problem, not the model's
		}
	}

	var v any
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("Validation failed for tool %q: arguments are not valid JSON: %v\n\nReceived arguments:\n%s", name, err, args)
	}

	v = coerce(v, s)
	var errs []string
	validate(v, s, "", &errs)
	if len(errs) > 0 {
		return nil, fmt.Errorf("Validation failed for tool %q:\n  - %s\n\nReceived arguments:\n%s", name, strings.Join(errs, "\n  - "), args)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func schemaTypes(s map[string]any) []string {
	switch t := s["type"].(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, x := range t {
			if str, ok := x.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

func hasType(s map[string]any, t string) bool {
	for _, x := range schemaTypes(s) {
		if x == t {
			return true
		}
	}
	return false
}

func sub(s map[string]any, key string) map[string]any {
	m, _ := s[key].(map[string]any)
	return m
}

// coerce fixes what a model tends to get wrong. It returns the value to use.
func coerce(v any, s map[string]any) any {
	if s == nil {
		return v
	}
	switch x := v.(type) {
	case map[string]any:
		props := sub(s, "properties")
		required := map[string]bool{}
		if r, ok := s["required"].([]any); ok {
			for _, n := range r {
				if str, ok := n.(string); ok {
					required[str] = true
				}
			}
		}
		for k, val := range x {
			ps := sub(props, k)
			if ps == nil {
				continue
			}
			if val == nil && !required[k] && !hasType(ps, "null") && len(schemaTypes(ps)) > 0 {
				delete(x, k) // null for an optional field means "not given"
				continue
			}
			x[k] = coerce(val, ps)
		}
	case []any:
		if is := sub(s, "items"); is != nil {
			for i := range x {
				x[i] = coerce(x[i], is)
			}
		}
	case string:
		if hasType(s, "string") {
			return x
		}
		if hasType(s, "number") || hasType(s, "integer") {
			if _, err := strconv.ParseFloat(x, 64); err == nil {
				return json.Number(strings.TrimSpace(x))
			}
		}
		if hasType(s, "boolean") {
			switch x {
			case "true":
				return true
			case "false":
				return false
			}
		}
	}
	return v
}

func kind(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return "integer"
		}
		if f, err := x.Float64(); err == nil && f == math.Trunc(f) && !math.IsInf(f, 0) {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func at(path string) string {
	if path == "" {
		return "root"
	}
	return path
}

func validate(v any, s map[string]any, path string, errs *[]string) {
	if s == nil {
		return
	}
	if types := schemaTypes(s); len(types) > 0 {
		k := kind(v)
		ok := false
		for _, t := range types {
			if t == k || (t == "number" && k == "integer") {
				ok = true
			}
		}
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected %s, got %s", at(path), strings.Join(types, " or "), k))
			return
		}
	}
	if enum, ok := s["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if fmt.Sprint(e) == fmt.Sprint(v) && kind(e) == kind(v) {
				found = true
			}
		}
		if !found {
			b, _ := json.Marshal(enum)
			*errs = append(*errs, fmt.Sprintf("%s: must be one of %s", at(path), b))
		}
	}
	switch x := v.(type) {
	case json.Number:
		f, _ := x.Float64()
		if m, ok := s["minimum"].(float64); ok && f < m {
			*errs = append(*errs, fmt.Sprintf("%s: %s is below the minimum %v", at(path), x, m))
		}
		if m, ok := s["maximum"].(float64); ok && f > m {
			*errs = append(*errs, fmt.Sprintf("%s: %s is above the maximum %v", at(path), x, m))
		}
	case map[string]any:
		props := sub(s, "properties")
		if r, ok := s["required"].([]any); ok {
			for _, n := range r {
				if name, ok := n.(string); ok {
					if _, present := x[name]; !present {
						*errs = append(*errs, fmt.Sprintf("%s: is required", join(path, name)))
					}
				}
			}
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if ps := sub(props, k); ps != nil {
				validate(x[k], ps, join(path, k), errs)
			} else if ap, ok := s["additionalProperties"].(bool); ok && !ap {
				*errs = append(*errs, fmt.Sprintf("%s: is not allowed", join(path, k)))
			}
		}
	case []any:
		if is := sub(s, "items"); is != nil {
			for i, item := range x {
				validate(item, is, join(path, strconv.Itoa(i)), errs)
			}
		}
	}
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}
