package projects

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestComputeServiceName_Comprehensive verifies service name generation.
func TestComputeServiceName_Comprehensive(t *testing.T) {
	tests := []struct {
		name     string
		project  string
		expected string
	}{
		{
			name:     "simple name",
			project:  "myproject",
			expected: "kubetty-project-myproject",
		},
		{
			name:     "short name",
			project:  "test",
			expected: "kubetty-project-test",
		},
		{
			name:     "name with dashes",
			project:  "my-project-name",
			expected: "kubetty-project-my-project-name",
		},
		{
			name:     "truncates long name",
			project:  "this-is-a-very-long-project-name-that-exceeds-maximum-length",
			expected: "kubetty-project-this-is-a-very-long-project-name-that-exceeds-m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComputeServiceName(tt.project)
			require.Equal(t, tt.expected, result)
			require.LessOrEqual(t, len(result), MaxServiceNameLength)
		})
	}
}

// TestCreateProjectRequest_ApplyDefaults verifies default values are applied.
func TestCreateProjectRequest_ApplyDefaults(t *testing.T) {
	t.Run("empty request gets all defaults", func(t *testing.T) {
		req := CreateProjectRequest{}
		req.ApplyDefaults()

		require.Equal(t, DefaultCPURequest, req.CPURequest)
		require.Equal(t, DefaultCPULimit, req.CPULimit)
		require.Equal(t, DefaultMemoryRequest, req.MemoryRequest)
		require.Equal(t, DefaultMemoryLimit, req.MemoryLimit)
		require.Equal(t, DefaultStorageSize, req.StorageSize)
		require.Equal(t, DefaultStorageClass, req.StorageClass)
		require.Equal(t, DefaultMaxTabsPerClient, req.MaxTabsPerClient)
		require.Equal(t, DefaultMaxTabsTotal, req.MaxTabsTotal)
		require.Equal(t, DefaultImageRepository, req.ImageRepository)
		require.Equal(t, DefaultImageTag, req.ImageTag)
		require.NotNil(t, req.DinDEnabled)
		require.True(t, *req.DinDEnabled)
		require.NotNil(t, req.AdminNamespaces)
		require.NotNil(t, req.ReadNamespaces)
		require.NotNil(t, req.EnvVars)
	})

	t.Run("preserves set values", func(t *testing.T) {
		dind := false
		req := CreateProjectRequest{
			CPURequest:       "1000m",
			CPULimit:         "2000m",
			MemoryRequest:    "1Gi",
			MemoryLimit:      "2Gi",
			StorageSize:      "100Gi",
			StorageClass:     "fast-ssd",
			MaxTabsPerClient: 5,
			MaxTabsTotal:     20,
			DinDEnabled:      &dind,
			ImageRepository:  "custom/image",
			ImageTag:         "v1.0.0",
			AdminNamespaces:  []string{"admin"},
			ReadNamespaces:   []string{"read"},
			EnvVars:          map[string]string{"KEY": "value"},
		}
		req.ApplyDefaults()

		require.Equal(t, "1000m", req.CPURequest)
		require.Equal(t, "2000m", req.CPULimit)
		require.Equal(t, "1Gi", req.MemoryRequest)
		require.Equal(t, "2Gi", req.MemoryLimit)
		require.Equal(t, "100Gi", req.StorageSize)
		require.Equal(t, "fast-ssd", req.StorageClass)
		require.Equal(t, 5, req.MaxTabsPerClient)
		require.Equal(t, 20, req.MaxTabsTotal)
		require.False(t, *req.DinDEnabled)
		require.Equal(t, "custom/image", req.ImageRepository)
		require.Equal(t, "v1.0.0", req.ImageTag)
		require.Equal(t, []string{"admin"}, req.AdminNamespaces)
		require.Equal(t, []string{"read"}, req.ReadNamespaces)
		require.Equal(t, map[string]string{"KEY": "value"}, req.EnvVars)
	})

	t.Run("partial defaults", func(t *testing.T) {
		req := CreateProjectRequest{
			CPURequest: "1000m",
		}
		req.ApplyDefaults()

		// Custom value preserved
		require.Equal(t, "1000m", req.CPURequest)
		// Default applied
		require.Equal(t, DefaultCPULimit, req.CPULimit)
	})
}

// TestProjectStatus constants are valid.
func TestProjectStatus(t *testing.T) {
	// Verify all status constants are defined
	statuses := []ProjectStatus{
		StatusPending,
		StatusSyncing,
		StatusCreating,
		StatusRunning,
		StatusUpdating,
		StatusFailed,
		StatusDeleting,
		StatusDeleted,
	}

	seen := make(map[ProjectStatus]bool)
	for _, status := range statuses {
		require.NotEmpty(t, string(status), "status should not be empty")
		require.False(t, seen[status], "duplicate status: %s", status)
		seen[status] = true
	}
}

// TestProjectDefaults verifies default constants.
func TestProjectDefaults(t *testing.T) {
	require.NotEmpty(t, DefaultCPURequest)
	require.NotEmpty(t, DefaultCPULimit)
	require.NotEmpty(t, DefaultMemoryRequest)
	require.NotEmpty(t, DefaultMemoryLimit)
	require.NotEmpty(t, DefaultStorageSize)
	require.NotEmpty(t, DefaultStorageClass)
	require.Greater(t, DefaultMaxTabsPerClient, 0)
	require.Greater(t, DefaultMaxTabsTotal, 0)
	require.NotEmpty(t, DefaultImageRepository)
	require.NotEmpty(t, DefaultImageTag)
}

// TestServiceNameConstants verifies service name constants.
func TestServiceNameConstants(t *testing.T) {
	require.Equal(t, "kubetty-project-", ServiceNamePrefix)
	require.Equal(t, 63, MaxServiceNameLength)
}

// TestListFilter defaults.
func TestListFilter(t *testing.T) {
	var filter ListFilter

	// Verify zero values
	require.Equal(t, ProjectStatus(""), filter.Status)
	require.Equal(t, "", filter.UserName)
	require.False(t, filter.IncludeAll)
	require.Equal(t, 0, filter.Limit)
	require.Equal(t, 0, filter.Offset)
}

// TestUpdateProjectRequest field types.
func TestUpdateProjectRequest(t *testing.T) {
	str := "test"
	num := 5
	flag := true

	req := UpdateProjectRequest{
		DisplayName:      &str,
		Description:      &str,
		Icon:             &str,
		CPURequest:       &str,
		CPULimit:         &str,
		MemoryRequest:    &str,
		MemoryLimit:      &str,
		StorageSize:      &str,
		MaxTabsPerClient: &num,
		MaxTabsTotal:     &num,
		DinDEnabled:      &flag,
		EnvVars:          map[string]string{"key": "value"},
		ImageTag:         &str,
	}

	require.NotNil(t, req.DisplayName)
	require.NotNil(t, req.Description)
	require.NotNil(t, req.Icon)
	require.NotNil(t, req.CPURequest)
	require.NotNil(t, req.CPULimit)
	require.NotNil(t, req.MemoryRequest)
	require.NotNil(t, req.MemoryLimit)
	require.NotNil(t, req.StorageSize)
	require.NotNil(t, req.MaxTabsPerClient)
	require.NotNil(t, req.MaxTabsTotal)
	require.NotNil(t, req.DinDEnabled)
	require.NotNil(t, req.EnvVars)
	require.NotNil(t, req.ImageTag)
}
