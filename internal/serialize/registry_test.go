package serialize

import "testing"

func TestLookupOpenTraces(t *testing.T) {
	serializer := Lookup("opentraces")
	if serializer == nil {
		t.Fatal(`Lookup("opentraces") = nil, want registered serializer`)
	}
	if serializer.Name() != "opentraces" {
		t.Errorf("Name() = %q, want %q", serializer.Name(), "opentraces")
	}
	if serializer.Description() == "" {
		t.Error("Description() is empty")
	}
}

func TestRegistryNamesAreUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for _, serializer := range Registry() {
		name := serializer.Name()
		if _, dup := seen[name]; dup {
			t.Errorf("duplicate serializer name %q", name)
		}
		seen[name] = struct{}{}
	}
}
