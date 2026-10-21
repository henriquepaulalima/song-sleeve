package position

import "testing"

func TestBetweenKnownValues(t *testing.T) {
	// Expected values from the reference fractional-indexing test suite.
	cases := []struct{ a, b, want string }{
		{"", "", "a0"},
		{"", "a0", "Zz"},
		{"", "Zz", "Zy"},
		{"a0", "", "a1"},
		{"a1", "", "a2"},
		{"a0", "a1", "a0V"},
		{"a1", "a2", "a1V"},
		{"a0V", "a1", "a0l"},
		{"Zz", "a0", "ZzV"},
		{"Zz", "a1", "a0"},
		{"", "Y00", "Xzzz"},
		{"bzz", "", "c000"},
		{"a0", "a0V", "a0G"},
		{"a0", "a0G", "a08"},
		{"b125", "b129", "b127"},
		{"a0", "a1V", "a1"},
		{"Zz", "a01", "a0"},
		{"", "a0V", "a0"},
		{"", "b999", "b99"},
	}
	for _, c := range cases {
		got, err := Between(c.a, c.b)
		if err != nil {
			t.Fatalf("Between(%q, %q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("Between(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestBetweenErrors(t *testing.T) {
	for _, c := range [][2]string{{"a0", "a0"}, {"a1", "a0"}, {"a00", ""}, {"a0", "a00"}} {
		if _, err := Between(c[0], c[1]); err == nil {
			t.Errorf("Between(%q, %q) should fail", c[0], c[1])
		}
	}
}

func TestManyAppendsStayOrdered(t *testing.T) {
	prev := ""
	for i := 0; i < 5000; i++ {
		k := After(prev)
		if prev != "" && k <= prev {
			t.Fatalf("key %q not after %q", k, prev)
		}
		prev = k
	}
}

func TestRepeatedInsertBetween(t *testing.T) {
	a, b := "a0", "a1"
	for i := 0; i < 200; i++ {
		m, err := Between(a, b)
		if err != nil {
			t.Fatal(err)
		}
		if !(a < m && m < b) {
			t.Fatalf("%q not between %q and %q", m, a, b)
		}
		b = m
	}
}
