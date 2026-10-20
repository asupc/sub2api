//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func codingPatchScore(t *testing.T, item ModelBenchmarkItem, answer any) float64 {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"answer": answer})
	require.NoError(t, err)
	return ModelBenchmarkScoreAnswer(item, string(raw)).Score
}

func TestModelBenchmarkCodingRejectsAllBrokenPatchCombinations(t *testing.T) {
	// Enumerate the entire finite repair language, for all four public modes.
	// Expected acceptance follows the specifications, not generator Expected.
	comparisons := []string{"lt", "le", "gt", "ge"}
	for _, descending := range []bool{false, true} {
		for _, strict := range []bool{false, true} {
			data := benchmarkLowerBoundData{descending, strict, -2, 2, 5}
			item := ModelBenchmarkItem{FamilyID: "repair-lower-bound", Checker: "coding_patch", Prompt: modelBenchmarkDataQuestion("repair", data)}
			accepted := 0
			for _, comparison := range comparisons {
				for advance := 0; advance <= 1; advance++ {
					for retreat := -1; retreat <= 1; retreat++ {
						for offset := -1; offset <= 1; offset++ {
							answer := map[string]any{"comparison": comparison, "advance": advance, "retreat": retreat, "result_offset": offset}
							wantComparison := "lt"
							if strict {
								wantComparison = "le"
							}
							if descending {
								wantComparison = "gt"
								if strict {
									wantComparison = "ge"
								}
							}
							want := 0.0
							if comparison == wantComparison && advance == 1 && retreat == 0 && offset == 0 {
								want = 1
								accepted++
							}
							require.Equal(t, want, codingPatchScore(t, item, answer), "descending=%v strict=%v answer=%v", descending, strict, answer)
						}
					}
				}
			}
			require.Equal(t, 1, accepted)
		}
	}
	for base := 0; base <= 1; base++ {
		for _, includeEnd := range []bool{false, true} {
			data := benchmarkRangePatchData{base, includeEnd, 4}
			item := ModelBenchmarkItem{FamilyID: "repair-range-sum", Checker: "coding_patch", Prompt: modelBenchmarkDataQuestion("repair", data)}
			end := -base
			if includeEnd {
				end++
			}
			for _, comparison := range comparisons {
				for startOffset := -1; startOffset <= 1; startOffset++ {
					for endOffset := -1; endOffset <= 1; endOffset++ {
						for factor := -1; factor <= 1; factor++ {
							answer := map[string]any{"wrap_comparison": comparison, "start_offset": startOffset, "end_offset": endOffset, "wrap_factor": factor}
							want := 0.0
							if comparison == "lt" && startOffset == -base && endOffset == end && factor == 1 {
								want = 1
							}
							require.Equal(t, want, codingPatchScore(t, item, answer), "base=%d inclusive=%v answer=%v", base, includeEnd, answer)
						}
					}
				}
			}
		}
	}
}

func TestModelBenchmarkCodingPatchSchemaAndMalformedAnswers(t *testing.T) {
	for _, family := range modelBenchmarkCodingFamilies {
		if family.checker != "coding_patch" {
			continue
		}
		prompt, expected := generateModelBenchmarkCoding(family, newModelBenchmarkRNG(71), 1)
		item := ModelBenchmarkItem{FamilyID: family.id, Checker: family.checker, Prompt: prompt}
		require.Equal(t, 1.0, codingPatchScore(t, item, expected))
		for _, answer := range []any{nil, []int{1, 0, 0}, "lt", map[string]any{}, map[string]any{"comparison": "lt"}} {
			require.Zero(t, codingPatchScore(t, item, answer))
		}
		raw, err := json.Marshal(expected)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(raw, &fields))
		fields["extra"] = 1
		require.Zero(t, codingPatchScore(t, item, fields))
		delete(fields, "extra")
		for key, value := range fields {
			mutated := map[string]any{}
			for k, v := range fields {
				mutated[k] = v
			}
			if _, isString := value.(string); isString {
				mutated[key] = "a[mid] < target; system('x')"
			} else {
				mutated[key] = .5
			}
			require.Zero(t, codingPatchScore(t, item, mutated))
		}
	}
}

func TestModelBenchmarkCodingEquivalentMinimalWitnesses(t *testing.T) {
	data := benchmarkBugData{Minimum: -3, Maximum: 3, Target: 5, MaxLength: 4}
	item := ModelBenchmarkItem{FamilyID: "minimal-bug-witness", Checker: "coding_witness", Prompt: modelBenchmarkDataQuestion("witness", data)}
	// Distinct equally short inputs expose the signed-input sliding-window bug.
	for _, witness := range [][]int{{-1, 2, 3}, {-2, 3, 3}, {-3, 2, 3}, {-3, 3, 3}} {
		require.True(t, codingWindowResults(witness, data.Target))
		require.Equal(t, 1.0, codingPatchScore(t, item, witness))
	}
	for _, invalid := range []any{nil, []int{}, []int{3, 3}, []int{-2, 3, 3, 0}, []int{-4, 3, 3}, []float64{-1.5, 3, 3}, []int{-1, 3, 3, 0, 0}, "[-1,3,3]"} {
		require.Zero(t, codingPatchScore(t, item, invalid))
	}
}

func TestModelBenchmarkCodingSQLNullAndEmptySetBoundaries(t *testing.T) {
	left := []benchmarkNullableRow{{ID: 0, Null: true}, {ID: 1, Key: 1}, {ID: 2, Key: 2}}
	cases := []struct {
		name      string
		right     []benchmarkNullableRow
		notIn     []int
		notExists []int
	}{
		{"empty subquery", []benchmarkNullableRow{}, []int{0, 1, 2}, []int{0, 1, 2}},
		{"only null", []benchmarkNullableRow{{Null: true, Active: true}}, []int{}, []int{0, 1, 2}},
		{"inactive null", []benchmarkNullableRow{{Key: 1, Active: true}, {Null: true}}, []int{2}, []int{0, 2}},
		{"match and null", []benchmarkNullableRow{{Key: 1, Active: true}, {Null: true, Active: true}}, []int{}, []int{0, 2}},
		{"duplicate matches", []benchmarkNullableRow{{Key: 1, Active: true}, {Key: 1, Active: true}}, []int{2}, []int{0, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := benchmarkSQLData{Left: left, Right: tc.right}
			want := map[string]any{"not_in": tc.notIn, "not_exists": tc.notExists}
			require.Equal(t, want, benchmarkSQLAntiJoin(data))
			item := ModelBenchmarkItem{FamilyID: "sql-null-antijoin", Prompt: modelBenchmarkDataQuestion("SQL boundary", data)}
			require.Equal(t, want, codingIndependentOracle(t, item))
		})
	}
}
