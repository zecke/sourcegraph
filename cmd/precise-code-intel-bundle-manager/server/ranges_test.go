package server

import (
	"reflect"
	"testing"
)

func TestFindRanges(t *testing.T) {
	ranges := []RangeData{
		{
			StartLine:      0,
			StartCharacter: 3,
			EndLine:        0,
			EndCharacter:   5,
		},
		{
			StartLine:      1,
			StartCharacter: 3,
			EndLine:        1,
			EndCharacter:   5,
		},
		{
			StartLine:      2,
			StartCharacter: 3,
			EndLine:        2,
			EndCharacter:   5,
		},
		{
			StartLine:      3,
			StartCharacter: 3,
			EndLine:        3,
			EndCharacter:   5,
		},
		{
			StartLine:      4,
			StartCharacter: 3,
			EndLine:        4,
			EndCharacter:   5,
		},
	}

	m := map[ID]RangeData{}
	for i, r := range ranges {
		m[ID(i)] = r
	}

	for i, r := range ranges {
		actual := findRanges(m, i, 4)
		expected := []RangeData{r}
		if !reflect.DeepEqual(actual, expected) {
			t.Errorf("unexpected result. want=%v have=%v", expected, actual)
		}
	}
}

func TestFindRangesOrder(t *testing.T) {
	ranges := []RangeData{
		{
			StartLine:      0,
			StartCharacter: 3,
			EndLine:        4,
			EndCharacter:   5,
		},
		{
			StartLine:      1,
			StartCharacter: 3,
			EndLine:        3,
			EndCharacter:   5,
		},
		{
			StartLine:      2,
			StartCharacter: 3,
			EndLine:        2,
			EndCharacter:   5,
		},
		{
			StartLine:      5,
			StartCharacter: 3,
			EndLine:        5,
			EndCharacter:   5,
		},
		{
			StartLine:      6,
			StartCharacter: 3,
			EndLine:        6,
			EndCharacter:   5,
		},
	}

	m := map[ID]RangeData{}
	for i, r := range ranges {
		m[ID(i)] = r
	}

	actual := findRanges(m, 2, 4)
	expected := []RangeData{ranges[2], ranges[1], ranges[0]}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("unexpected result. want=%v have=%v", expected, actual)
	}

}

func TestComparePosition(t *testing.T) {
	left := RangeData{
		StartLine:      5,
		StartCharacter: 11,
		EndLine:        5,
		EndCharacter:   13,
	}

	testCases := []struct {
		line      int
		character int
		expected  int
	}{
		{5, 11, 0},
		{5, 12, 0},
		{5, 13, 0},
		{4, 12, +1},
		{5, 10, +1},
		{5, 14, -1},
		{6, 12, -1},
	}

	for _, testCase := range testCases {
		if cmp := comparePosition(left, testCase.line, testCase.character); cmp != testCase.expected {
			t.Errorf("unexpected comparison %d:%d. want=%d have=%d", testCase.line, testCase.character, testCase.expected, cmp)
		}
	}
}
