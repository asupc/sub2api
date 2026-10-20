package service

import "fmt"

type benchmarkLowerBoundData struct {
	Descending bool `json:"descending"`
	Strict     bool `json:"strict"`
	Minimum    int  `json:"minimum"`
	Maximum    int  `json:"maximum"`
	MaxLength  int  `json:"max_length"`
}
type benchmarkRangePatchData struct {
	IndexBase  int  `json:"index_base"`
	IncludeEnd bool `json:"include_end"`
	MaxLength  int  `json:"max_length"`
}
type benchmarkBugData struct {
	Minimum   int `json:"minimum"`
	Maximum   int `json:"maximum"`
	Target    int `json:"target"`
	MaxLength int `json:"max_length"`
}

func benchmarkCodingComparison(name string, a, b int) bool {
	switch name {
	case "lt":
		return a < b
	case "le":
		return a <= b
	case "gt":
		return a > b
	case "ge":
		return a >= b
	}
	return false
}

func benchmarkCodingPatchFields(answer any, comparisonKey string, integerKeys ...string) (string, []int, error) {
	object, ok := answer.(map[string]any)
	if !ok || len(object) != len(integerKeys)+1 {
		return "", nil, fmt.Errorf("patch fields mismatch")
	}
	comparison, ok := object[comparisonKey].(string)
	if !ok || (comparison != "lt" && comparison != "le" && comparison != "gt" && comparison != "ge") {
		return "", nil, fmt.Errorf("comparison must be lt/le/gt/ge")
	}
	values := make([]int, len(integerKeys))
	for i, key := range integerKeys {
		value, ok := modelBenchmarkJSONInt(object[key])
		if !ok || value < -1 || value > 1 {
			return "", nil, fmt.Errorf("%s must be an integer in -1..1", key)
		}
		values[i] = int(value)
	}
	return comparison, values, nil
}

// The repair language has four comparisons and small integer holes, with no
// eval, compiler, subprocess or model-supplied executable source.
func scoreModelBenchmarkCoding(item ModelBenchmarkItem, answer any) (float64, string) {
	switch item.FamilyID {
	case "repair-lower-bound":
		var data benchmarkLowerBoundData
		if err := modelBenchmarkQuestionData(item, &data); err != nil {
			return 0, err.Error()
		}
		comparison, values, err := benchmarkCodingPatchFields(answer, "comparison", "advance", "retreat", "result_offset")
		if err != nil {
			return 0, err.Error()
		}
		if values[0] < 0 {
			return 0, "advance must be 0 or 1"
		}
		if data.MaxLength < 0 || data.MaxLength > 5 || data.Minimum < -3 || data.Maximum > 3 || data.Minimum > data.Maximum {
			return 0, "invalid lower-bound input domain"
		}
		valid := true
		var arrays func([]int, int)
		arrays = func(ascending []int, length int) {
			if !valid {
				return
			}
			if len(ascending) < length {
				start := data.Minimum
				if len(ascending) > 0 {
					start = ascending[len(ascending)-1]
				}
				for value := start; value <= data.Maximum; value++ {
					arrays(append(ascending, value), length)
				}
				return
			}
			a := append([]int{}, ascending...)
			if data.Descending {
				for left, right := 0, len(a)-1; left < right; left, right = left+1, right-1 {
					a[left], a[right] = a[right], a[left]
				}
			}
			for target := data.Minimum - 1; target <= data.Maximum+1; target++ {
				want := len(a)
				for i, value := range a {
					holds := value >= target
					if data.Strict {
						holds = value > target
					}
					if data.Descending {
						holds = value <= target
						if data.Strict {
							holds = value < target
						}
					}
					if holds {
						want = i
						break
					}
				}
				lo, hi, steps := 0, len(a), 0
				for lo < hi && steps < 32 {
					mid := (lo + hi) / 2
					if lo < 0 || hi > len(a) || mid < 0 || mid >= len(a) {
						valid = false
						return
					}
					if benchmarkCodingComparison(comparison, a[mid], target) {
						lo = mid + values[0]
					} else {
						hi = mid + values[1]
					}
					steps++
				}
				if lo < hi || lo+values[2] != want {
					valid = false
					return
				}
			}
		}
		for length := 0; length <= data.MaxLength; length++ {
			arrays(nil, length)
		}
		if valid {
			return 1, ""
		}
		return 0, "patch fails a boundary, duplicate-value, termination or bounds case"
	case "repair-range-sum":
		var data benchmarkRangePatchData
		if err := modelBenchmarkQuestionData(item, &data); err != nil {
			return 0, err.Error()
		}
		comparison, values, err := benchmarkCodingPatchFields(answer, "wrap_comparison", "start_offset", "end_offset", "wrap_factor")
		if err != nil {
			return 0, err.Error()
		}
		if data.MaxLength < 1 || data.MaxLength > 4 || (data.IndexBase != 0 && data.IndexBase != 1) {
			return 0, "invalid range-sum input domain"
		}
		for n := 1; n <= data.MaxLength; n++ {
			combinations := 1
			for i := 0; i < n; i++ {
				combinations *= 3
			}
			for code := 0; code < combinations; code++ {
				a, prefix, digits := make([]int, n), make([]int, n+1), code
				for i := range a {
					a[i], digits = digits%3-1, digits/3
					prefix[i+1] = prefix[i] + a[i]
				}
				for left := 0; left < n; left++ {
					for right := 0; right < n; right++ {
						start, end := left+data.IndexBase, right+data.IndexBase
						p, q := start+values[0], end+values[1]
						if p < 0 || p > n || q < 0 || q > n {
							return 0, "patch indexes outside prefix array"
						}
						got := prefix[q] - prefix[p]
						if benchmarkCodingComparison(comparison, end, start) {
							got += values[2] * prefix[n]
						}
						want := 0
						for index := left; index != right; index = (index + 1) % n {
							want += a[index]
						}
						if data.IncludeEnd {
							want += a[right]
						}
						if got != want {
							return 0, "patch fails an empty, singleton, wraparound or full-range case"
						}
					}
				}
			}
		}
		return 1, ""
	case "minimal-bug-witness":
		var data benchmarkBugData
		if err := modelBenchmarkQuestionData(item, &data); err != nil {
			return 0, err.Error()
		}
		raw, ok := modelBenchmarkNormalizeList(answer)
		if !ok || len(raw) == 0 || len(raw) > data.MaxLength {
			return 0, "witness must be a nonempty array within max_length"
		}
		values := make([]int, len(raw))
		for i, entry := range raw {
			value, ok := modelBenchmarkJSONInt(entry)
			if !ok || value < int64(data.Minimum) || value > int64(data.Maximum) {
				return 0, "witness element outside integer domain"
			}
			values[i] = int(value)
		}
		if benchmarkBuggyWindow(values, data.Target) == benchmarkShortestSubarray(values, data.Target) {
			return 0, "input does not expose the bug"
		}
		if len(values) != len(benchmarkFindBugWitness(data)) {
			return 0, "counterexample is not minimum length"
		}
		return 1, ""
	}
	return 0, "unsupported coding checker family"
}

func benchmarkBuggyWindow(values []int, target int) int {
	left, sum, best := 0, 0, len(values)+1
	for right, value := range values {
		sum += value
		for sum >= target {
			best = min(best, right-left+1)
			sum -= values[left]
			left++
		}
	}
	if best > len(values) {
		return 0
	}
	return best
}

func benchmarkShortestSubarray(values []int, target int) int {
	best := len(values) + 1
	for left := range values {
		sum := 0
		for right := left; right < len(values); right++ {
			sum += values[right]
			if sum >= target {
				best = min(best, right-left+1)
			}
		}
	}
	if best > len(values) {
		return 0
	}
	return best
}

func benchmarkFindBugWitness(data benchmarkBugData) []int {
	for length := 1; length <= data.MaxLength; length++ {
		values := make([]int, length)
		var found []int
		var visit func(int)
		visit = func(index int) {
			if found != nil {
				return
			}
			if index == length {
				if benchmarkBuggyWindow(values, data.Target) != benchmarkShortestSubarray(values, data.Target) {
					found = append([]int{}, values...)
				}
				return
			}
			for value := data.Minimum; value <= data.Maximum; value++ {
				values[index] = value
				visit(index + 1)
			}
		}
		visit(0)
		if found != nil {
			return found
		}
	}
	panic("generated sliding-window domain has no counterexample")
}
