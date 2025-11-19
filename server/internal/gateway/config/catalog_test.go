package config

import "testing"

func TestParseCatalog(t *testing.T) {
	raw := []byte(`projects:
  - id: proj-a
    displayName: Project A
    namespace: ns-a
    service: svc-a
    port: 9090
  - id: proj-b
    displayName: Project B
    namespace: ns-b
    service: svc-b
`)
	cat, err := ParseCatalog(raw)
	if err != nil {
		t.Fatalf("ParseCatalog returned error: %v", err)
	}
	if len(cat.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(cat.Projects))
	}
	if cat.Projects[0].Port != 9090 {
		t.Fatalf("expected port 9090, got %d", cat.Projects[0].Port)
	}
	if cat.Projects[1].Port != defaultServicePort {
		t.Fatalf("expected default port %d, got %d", defaultServicePort, cat.Projects[1].Port)
	}
}

func TestParseCatalogValidation(t *testing.T) {
	raw := []byte(`projects:
  - id: "Bad ID"
    namespace: ns
    service: svc
`)
	if _, err := ParseCatalog(raw); err == nil {
		t.Fatalf("expected validation error, got nil")
	}
}

func TestIsValidProjectID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"proj-1", true},
		{"Proj", false},
		{"proj_1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isValidProjectID(tc.id); got != tc.want {
			t.Fatalf("isValidProjectID(%q)=%v want %v", tc.id, got, tc.want)
		}
	}
}
