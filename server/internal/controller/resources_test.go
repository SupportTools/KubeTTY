package controller

import (
	"testing"

	"github.com/google/uuid"
	"github.com/supporttools/KubeTTY/server/internal/projects"
)

// testProject creates a sample project for testing.
func testProject(name string) *projects.Project {
	return &projects.Project{
		ID:              uuid.New(),
		Name:            name,
		DisplayName:     "Test Project",
		Description:     "A test project",
		TargetNamespace: "test-ns",
		SessionID:       uuid.New(),
		UserName:        "testuser",
		CPURequest:      "500m",
		CPULimit:        "4000m",
		MemoryRequest:   "2Gi",
		MemoryLimit:     "8Gi",
		StorageSize:     "50Gi",
		StorageClass:    "freenas-iscsi-csi",
		ImageRepository: "harbor.support.tools/kubetty/kubetty",
		ImageTag:        "latest",
		DinDEnabled:     false,
		EnvVars:         map[string]string{},
	}
}

// testConfig creates a sample ResourceConfig for testing.
func testConfig() ResourceConfig {
	return ResourceConfig{
		Namespace: "kubetty-projects-dev",
		Prefix:    "kubetty-project-",
		Env:       "dev",
	}
}

func TestResourceConfig_ResourceName(t *testing.T) {
	tests := []struct {
		name        string
		cfg         ResourceConfig
		projectName string
		expected    string
	}{
		{
			name:        "standard naming",
			cfg:         ResourceConfig{Prefix: "kubetty-project-"},
			projectName: "alpha",
			expected:    "kubetty-project-alpha",
		},
		{
			name:        "different prefix",
			cfg:         ResourceConfig{Prefix: "proj-"},
			projectName: "myproject",
			expected:    "proj-myproject",
		},
		{
			name:        "empty prefix",
			cfg:         ResourceConfig{Prefix: ""},
			projectName: "standalone",
			expected:    "standalone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cfg.ResourceName(tt.projectName)
			if result != tt.expected {
				t.Errorf("ResourceName() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestResourceConfig_PVCName(t *testing.T) {
	tests := []struct {
		name        string
		cfg         ResourceConfig
		projectName string
		expected    string
	}{
		{
			name:        "default suffix",
			cfg:         ResourceConfig{Prefix: "kubetty-project-"},
			projectName: "alpha",
			expected:    "kubetty-project-alpha-data",
		},
		{
			name:        "custom suffix for truenas migration",
			cfg:         ResourceConfig{Prefix: "kubetty-project-", PVCSuffix: "-data-truenas"},
			projectName: "dr-syncer",
			expected:    "kubetty-project-dr-syncer-data-truenas",
		},
		{
			name:        "empty suffix defaults to -data",
			cfg:         ResourceConfig{Prefix: "kubetty-project-", PVCSuffix: ""},
			projectName: "beta",
			expected:    "kubetty-project-beta-data",
		},
		{
			name:        "custom suffix with different prefix",
			cfg:         ResourceConfig{Prefix: "proj-", PVCSuffix: "-storage"},
			projectName: "myapp",
			expected:    "proj-myapp-storage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cfg.PVCName(tt.projectName)
			if result != tt.expected {
				t.Errorf("PVCName() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase passes through",
			input:    "myproject",
			expected: "myproject",
		},
		{
			name:     "uppercase converted to lowercase",
			input:    "MyProject",
			expected: "myproject",
		},
		{
			name:     "underscores converted to dashes",
			input:    "my_project_name",
			expected: "my-project-name",
		},
		{
			name:     "special characters removed",
			input:    "my@project!name",
			expected: "myprojectname",
		},
		{
			name:     "leading dash removed",
			input:    "-myproject",
			expected: "myproject",
		},
		{
			name:     "trailing dash removed",
			input:    "myproject-",
			expected: "myproject",
		},
		{
			name:     "consecutive dashes collapsed",
			input:    "my--project",
			expected: "my-project",
		},
		{
			name:     "complex mixed input",
			input:    "_My_Test__Project!@#123_",
			expected: "my-test-project123",
		},
		{
			name:     "numbers preserved",
			input:    "project123",
			expected: "project123",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeName(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeName(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildPVC(t *testing.T) {
	p := testProject("alpha")
	cfg := testConfig()

	pvc := BuildPVC(p, cfg)

	// Test name format: {prefix}{project-name}-data
	expectedName := "kubetty-project-alpha-data"
	if pvc.Name != expectedName {
		t.Errorf("PVC name = %q, expected %q", pvc.Name, expectedName)
	}

	// Test namespace
	if pvc.Namespace != cfg.Namespace {
		t.Errorf("PVC namespace = %q, expected %q", pvc.Namespace, cfg.Namespace)
	}

	// Test labels
	verifyLabels(t, pvc.Labels, p, cfg, "PVC")

	// Test storage size
	storageQty := pvc.Spec.Resources.Requests["storage"]
	if storageQty.String() != p.StorageSize {
		t.Errorf("PVC storage = %q, expected %q", storageQty.String(), p.StorageSize)
	}

	// Test storage class
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != p.StorageClass {
		t.Errorf("PVC storageClass = %v, expected %q", pvc.Spec.StorageClassName, p.StorageClass)
	}
}

func TestBuildPVC_CustomSuffix(t *testing.T) {
	p := testProject("dr-syncer")
	cfg := testConfig()
	cfg.PVCSuffix = "-data-truenas"

	pvc := BuildPVC(p, cfg)

	expectedName := "kubetty-project-dr-syncer-data-truenas"
	if pvc.Name != expectedName {
		t.Errorf("PVC name = %q, expected %q", pvc.Name, expectedName)
	}
}

func TestBuildDeployment_CustomPVCSuffix(t *testing.T) {
	p := testProject("fruition")
	cfg := testConfig()
	cfg.PVCSuffix = "-data-truenas"

	deploy := BuildDeployment(p, cfg, "")

	expectedPVCName := "kubetty-project-fruition-data-truenas"
	foundPVC := false
	for _, vol := range deploy.Spec.Template.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == expectedPVCName {
			foundPVC = true
			break
		}
	}
	if !foundPVC {
		t.Errorf("Deployment should reference PVC %q", expectedPVCName)
	}
}

func TestBuildServiceAccount(t *testing.T) {
	p := testProject("beta")
	cfg := testConfig()

	sa := BuildServiceAccount(p, cfg)

	// Test name format: {prefix}{project-name}-sa
	expectedName := "kubetty-project-beta-sa"
	if sa.Name != expectedName {
		t.Errorf("ServiceAccount name = %q, expected %q", sa.Name, expectedName)
	}

	// Test namespace
	if sa.Namespace != cfg.Namespace {
		t.Errorf("ServiceAccount namespace = %q, expected %q", sa.Namespace, cfg.Namespace)
	}

	// Test labels
	verifyLabels(t, sa.Labels, p, cfg, "ServiceAccount")
}

func TestBuildService(t *testing.T) {
	p := testProject("gamma")
	cfg := testConfig()

	svc := BuildService(p, cfg)

	// Test name format: {prefix}{project-name}
	expectedName := "kubetty-project-gamma"
	if svc.Name != expectedName {
		t.Errorf("Service name = %q, expected %q", svc.Name, expectedName)
	}

	// Test namespace
	if svc.Namespace != cfg.Namespace {
		t.Errorf("Service namespace = %q, expected %q", svc.Namespace, cfg.Namespace)
	}

	// Test labels
	verifyLabels(t, svc.Labels, p, cfg, "Service")

	// Test selector labels
	verifySelectorLabels(t, svc.Spec.Selector, p, "Service")

	// Test port
	if len(svc.Spec.Ports) != 1 {
		t.Errorf("Service should have 1 port, got %d", len(svc.Spec.Ports))
	} else if svc.Spec.Ports[0].Port != 8080 {
		t.Errorf("Service port = %d, expected 8080", svc.Spec.Ports[0].Port)
	}
}

func TestBuildDeployment(t *testing.T) {
	p := testProject("delta")
	cfg := testConfig()
	envSecretName := "delta-env-secret"

	deploy := BuildDeployment(p, cfg, envSecretName)

	// Test name format: {prefix}{project-name}
	expectedName := "kubetty-project-delta"
	if deploy.Name != expectedName {
		t.Errorf("Deployment name = %q, expected %q", deploy.Name, expectedName)
	}

	// Test namespace
	if deploy.Namespace != cfg.Namespace {
		t.Errorf("Deployment namespace = %q, expected %q", deploy.Namespace, cfg.Namespace)
	}

	// Test labels
	verifyLabels(t, deploy.Labels, p, cfg, "Deployment")

	// Test pod template labels (selector labels)
	verifySelectorLabels(t, deploy.Spec.Template.Labels, p, "Deployment PodTemplate")

	// Test ServiceAccount reference
	expectedSAName := "kubetty-project-delta-sa"
	if deploy.Spec.Template.Spec.ServiceAccountName != expectedSAName {
		t.Errorf("Deployment SA = %q, expected %q",
			deploy.Spec.Template.Spec.ServiceAccountName, expectedSAName)
	}

	// Test PVC reference in volumes
	foundPVC := false
	expectedPVCName := "kubetty-project-delta-data"
	for _, vol := range deploy.Spec.Template.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == expectedPVCName {
			foundPVC = true
			break
		}
	}
	if !foundPVC {
		t.Errorf("Deployment should reference PVC %q", expectedPVCName)
	}

	// Test secret reference in envFrom
	foundEnvFrom := false
	for _, container := range deploy.Spec.Template.Spec.Containers {
		if container.Name == "kubetty" {
			for _, envFrom := range container.EnvFrom {
				if envFrom.SecretRef != nil && envFrom.SecretRef.Name == envSecretName {
					foundEnvFrom = true
					break
				}
			}
		}
	}
	if !foundEnvFrom {
		t.Errorf("Deployment should reference secret %q in envFrom", envSecretName)
	}
}

func TestBuildDeployment_WithoutEnvSecret(t *testing.T) {
	p := testProject("epsilon")
	cfg := testConfig()

	deploy := BuildDeployment(p, cfg, "")

	// Verify no envFrom when secret name is empty
	for _, container := range deploy.Spec.Template.Spec.Containers {
		if container.Name == "kubetty" {
			if len(container.EnvFrom) > 0 {
				t.Error("Deployment should not have envFrom when secret name is empty")
			}
		}
	}
}

func TestBuildDeployment_WithDinD(t *testing.T) {
	p := testProject("zeta")
	p.DinDEnabled = true
	cfg := testConfig()

	deploy := BuildDeployment(p, cfg, "")

	// Verify docker sidecar container exists
	foundDocker := false
	for _, container := range deploy.Spec.Template.Spec.Containers {
		if container.Name == "docker" {
			foundDocker = true
			break
		}
	}
	if !foundDocker {
		t.Error("Deployment with DinD enabled should have docker sidecar container")
	}

	// Verify dind-sock volume exists
	foundDindSock := false
	for _, vol := range deploy.Spec.Template.Spec.Volumes {
		if vol.Name == "dind-sock" {
			foundDindSock = true
			break
		}
	}
	if !foundDindSock {
		t.Error("Deployment with DinD enabled should have dind-sock volume")
	}
}

func TestProjectLabels(t *testing.T) {
	p := testProject("lambda")
	cfg := testConfig()

	labels := projectLabels(p, cfg)

	// Verify all expected labels
	expectedLabels := map[string]string{
		"app.kubernetes.io/name":       "kubetty",
		"app.kubernetes.io/instance":   "lambda",
		"app.kubernetes.io/managed-by": "kubetty-controller",
		"app.kubernetes.io/component":  "project",
		"kubetty.io/environment":       "dev",
		"kubetty.io/project":           "lambda",
	}

	for key, expected := range expectedLabels {
		if got := labels[key]; got != expected {
			t.Errorf("Label %q = %q, expected %q", key, got, expected)
		}
	}
}

func TestProjectSelectorLabels(t *testing.T) {
	p := testProject("mu")
	cfg := testConfig()

	labels := projectSelectorLabels(p, cfg)

	// Verify all expected selector labels
	expectedLabels := map[string]string{
		"app.kubernetes.io/name":     "kubetty",
		"app.kubernetes.io/instance": "mu",
		"kubetty.io/project":         "mu",
	}

	for key, expected := range expectedLabels {
		if got := labels[key]; got != expected {
			t.Errorf("Selector label %q = %q, expected %q", key, got, expected)
		}
	}

	// Selector labels should be minimal (only 3 labels)
	if len(labels) != 3 {
		t.Errorf("Selector labels should have 3 entries, got %d", len(labels))
	}
}

// Helper functions

func verifyLabels(t *testing.T, labels map[string]string, p *projects.Project, cfg ResourceConfig, resourceType string) {
	t.Helper()

	requiredLabels := []string{
		labelApp,
		labelInstance,
		labelManagedBy,
		labelComponent,
		labelEnvironment,
		labelProject,
	}

	for _, key := range requiredLabels {
		if _, exists := labels[key]; !exists {
			t.Errorf("%s missing label %q", resourceType, key)
		}
	}

	// Verify specific values
	if labels[labelApp] != "kubetty" {
		t.Errorf("%s label %s = %q, expected 'kubetty'", resourceType, labelApp, labels[labelApp])
	}
	if labels[labelInstance] != p.Name {
		t.Errorf("%s label %s = %q, expected %q", resourceType, labelInstance, labels[labelInstance], p.Name)
	}
	if labels[labelManagedBy] != managedByValue {
		t.Errorf("%s label %s = %q, expected %q", resourceType, labelManagedBy, labels[labelManagedBy], managedByValue)
	}
	if labels[labelEnvironment] != cfg.Env {
		t.Errorf("%s label %s = %q, expected %q", resourceType, labelEnvironment, labels[labelEnvironment], cfg.Env)
	}
	if labels[labelProject] != p.Name {
		t.Errorf("%s label %s = %q, expected %q", resourceType, labelProject, labels[labelProject], p.Name)
	}
}

func verifySelectorLabels(t *testing.T, labels map[string]string, p *projects.Project, resourceType string) {
	t.Helper()

	requiredLabels := []string{labelApp, labelInstance, labelProject}

	for _, key := range requiredLabels {
		if _, exists := labels[key]; !exists {
			t.Errorf("%s selector missing label %q", resourceType, key)
		}
	}

	if labels[labelApp] != "kubetty" {
		t.Errorf("%s selector label %s = %q, expected 'kubetty'", resourceType, labelApp, labels[labelApp])
	}
	if labels[labelInstance] != p.Name {
		t.Errorf("%s selector label %s = %q, expected %q", resourceType, labelInstance, labels[labelInstance], p.Name)
	}
	if labels[labelProject] != p.Name {
		t.Errorf("%s selector label %s = %q, expected %q", resourceType, labelProject, labels[labelProject], p.Name)
	}
}
