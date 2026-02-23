package subnet

import (
	"reflect"
	"testing"
)

func TestUniqueSortedStrings(t *testing.T) {
	in := []string{"10.0.0.0/24", "", "10.0.0.0/24", "10.2.0.0/16", "10.1.0.0/16"}
	got := uniqueSortedStrings(in)
	want := []string{"10.0.0.0/24", "10.1.0.0/16", "10.2.0.0/16"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueSortedStrings() = %#v, want %#v", got, want)
	}
}

func TestMapKeysSorted(t *testing.T) {
	in := map[string]string{
		"10.2.0.0/16": "peer-b",
		"":            "ignore",
		"10.0.0.0/24": "peer-a",
		"10.1.0.0/16": "peer-c",
	}
	got := mapKeysSorted(in)
	want := []string{"10.0.0.0/24", "10.1.0.0/16", "10.2.0.0/16"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapKeysSorted() = %#v, want %#v", got, want)
	}
}
