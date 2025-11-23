package projects

import "testing"

// TestComputeServiceName verifies service name generation.
func TestComputeServiceName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Short name",
			input:    "myproject",
			expected: "kubetty-project-myproject",
		},
		{
			name:     "Single character",
			input:    "a",
			expected: "kubetty-project-a",
		},
		{
			name:     "Name with dashes",
			input:    "my-cool-project",
			expected: "kubetty-project-my-cool-project",
		},
		{
			name:     "Maximum length before truncation (47 chars)",
			input:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", // 47 chars
			expected: "kubetty-project-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			name:     "Exceeds 63 char limit - should truncate",
			input:    "this-is-a-very-long-project-name-that-exceeds-the-kubernetes-limit",
			expected: "kubetty-project-this-is-a-very-long-project-name-that-exceeds-t",
		},
		{
			name:     "Exactly at max length (name = 47 chars)",
			input:    "12345678901234567890123456789012345678901234567",
			expected: "kubetty-project-12345678901234567890123456789012345678901234567",
		},
		{
			name:     "One over max length (name = 48 chars)",
			input:    "123456789012345678901234567890123456789012345678",
			expected: "kubetty-project-12345678901234567890123456789012345678901234567",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComputeServiceName(tt.input)
			if result != tt.expected {
				t.Errorf("ComputeServiceName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
			if len(result) > MaxServiceNameLength {
				t.Errorf("ComputeServiceName(%q) length = %d, exceeds max %d", tt.input, len(result), MaxServiceNameLength)
			}
		})
	}
}

// TestComputeServiceNameLength verifies that result never exceeds 63 chars.
func TestComputeServiceNameLength(t *testing.T) {
	// Test with various lengths
	for i := 1; i <= 100; i++ {
		name := make([]byte, i)
		for j := range name {
			name[j] = 'a'
		}
		result := ComputeServiceName(string(name))
		if len(result) > MaxServiceNameLength {
			t.Errorf("ComputeServiceName with %d char input produced %d char output, exceeds %d",
				i, len(result), MaxServiceNameLength)
		}
	}
}

// TestServiceNamePrefix verifies the prefix constant.
func TestServiceNamePrefix(t *testing.T) {
	if ServiceNamePrefix != "kubetty-project-" {
		t.Errorf("ServiceNamePrefix = %q, want %q", ServiceNamePrefix, "kubetty-project-")
	}
	if len(ServiceNamePrefix) != 16 {
		t.Errorf("ServiceNamePrefix length = %d, want 16", len(ServiceNamePrefix))
	}
}

// TestMaxServiceNameLength verifies the max length constant.
func TestMaxServiceNameLength(t *testing.T) {
	if MaxServiceNameLength != 63 {
		t.Errorf("MaxServiceNameLength = %d, want 63", MaxServiceNameLength)
	}
}
