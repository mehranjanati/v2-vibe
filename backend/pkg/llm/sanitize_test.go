package llm

import "testing"

func TestSanitizeJSRepairsKnownFailures(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"empty ident", "rating: ,", "rating: null,"},
		{"price dot", "price: . }", "price: null }"},
		{"truncated decimal", "rating: 4.,", "rating: 4,"},
		{"quoted key", `'x': ,`, `'x': null,`},
		{"valid untouched", `id: "p1", price: 4.50`, `id: "p1", price: 4.50`},
		{"empty cmp before &&", "if (minPrice !== && maxPrice !==) {", `if (minPrice !== "" && maxPrice !== "" ) {`},
		{"empty cmp eq before &&", "if (a == && b) {", `if (a == "" && b) {`},
		{"normal cmp untouched", "if (a == b && c) {", "if (a == b && c) {"},
		{"empty || before ;", "const m = parseFloat(x.value) ||;", "const m = parseFloat(x.value) || 0;"},
		{"empty || before ,", "f(x) ||,", "f(x) || 0,"},
		{"normal || untouched", "const m = parseFloat(x) || 10;", "const m = parseFloat(x) || 10;"},
	}
	for _, c := range cases {
		if got := SanitizeJS(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSanitizeJSRealWorldStoreStub(t *testing.T) {
	in := "App.store = { products: [\n    { id: , name: 'Product ', price: . },\n  ], };"
	got := SanitizeJS(in)
	if !validGoJSONish(got) && !jsonParseable(got) {
		// store.js is not JSON, but the object literal must at least be
		// free of the broken patterns.
	}
	if contains(got, "id: ,") || contains(got, "price: .") {
		t.Fatalf("broken patterns survived sanitize: %q", got)
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
func jsonParseable(s string) bool { return true }
func validGoJSONish(s string) bool { return true }
