package alarms

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var clauseRE = regexp.MustCompile(`^\s*device\.(\w+)\s*(>=|<=|==|!=|>|<)\s*(?:"([^"]*)"|\s*([\d.+-eE]+))\s*$`)

// EvalMini evaluates a constrained DSL used by alarm_rules.expression:
//
//	device.FIELD cmp VALUE joined by && (no parentheses yet).
//
// Supported fields: cpu_pct (float), mem_used_ratio (float), criticality (string), vendor (string).
func EvalMini(expr string, env map[string]any) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" || expr == "false" {
		return false, nil
	}
	if expr == "true" {
		return true, nil
	}
	for _, part := range strings.Split(expr, "&&") {
		part = strings.TrimSpace(part)
		ok, err := evalClause(part, env)
		if err != nil || !ok {
			return ok, err
		}
	}
	return true, nil
}

func evalClause(clause string, env map[string]any) (bool, error) {
	m := clauseRE.FindStringSubmatch(clause)
	if m == nil {
		return false, fmt.Errorf("alarms: unsupported clause %q", clause)
	}
	field := m[1]
	op := m[2]
	qStr := m[3]
	numStr := strings.TrimSpace(m[4])

	rv, ok := env[field]
	if !ok {
		return false, nil
	}

	switch field {
	case "criticality", "vendor":
		ls, ok1 := rv.(string)
		if !ok1 || qStr == "" {
			return false, fmt.Errorf("alarms: string compare requires quoted RHS for %s", field)
		}
		return compareString(ls, op, qStr), nil
	default:
		lf, err := toFloat64(rv)
		if err != nil {
			return false, err
		}
		if numStr == "" {
			return false, fmt.Errorf("alarms: numeric RHS required for %s", field)
		}
		rf, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return false, fmt.Errorf("alarms: float RHS: %w", err)
		}
		return compareFloat(lf, op, rf), nil
	}
}

func toFloat64(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case uint64:
		return float64(x), nil
	default:
		return 0, fmt.Errorf("alarms: %T not numeric", v)
	}
}

func compareFloat(l float64, op string, r float64) bool {
	switch op {
	case ">":
		return l > r
	case ">=":
		return l >= r
	case "<":
		return l < r
	case "<=":
		return l <= r
	case "==":
		return l == r
	case "!=":
		return l != r
	default:
		return false
	}
}

func compareString(l, op, r string) bool {
	switch op {
	case "==":
		return l == r
	case "!=":
		return l != r
	default:
		return false
	}
}
