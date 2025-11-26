package controller

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/supporttools/KubeTTY/server/internal/projects"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// Standard Kubernetes labels
	labelApp       = "app.kubernetes.io/name"
	labelInstance  = "app.kubernetes.io/instance"
	labelManagedBy = "app.kubernetes.io/managed-by"
	labelComponent = "app.kubernetes.io/component"

	// KubeTTY-specific labels
	labelEnvironment = "kubetty.io/environment"
	labelProject     = "kubetty.io/project"

	managedByValue = "kubetty-controller"
)

// ResourceConfig holds configuration for building Kubernetes resources.
// It provides the shared namespace, resource prefix, and environment suffix
// needed for single-namespace project management.
type ResourceConfig struct {
	// Namespace is the shared namespace for all project resources
	// (e.g., "kubetty-projects-dev")
	Namespace string

	// Prefix is prepended to all resource names to avoid collisions
	// (e.g., "kubetty-project-")
	Prefix string

	// Env is the environment suffix parsed from the namespace
	// (e.g., "dev", "prd") - used for cluster-scoped resources
	Env string

	// ImagePullSecrets lists secret names for pulling container images
	// (e.g., ["harbor-supporttools"])
	ImagePullSecrets []string

	// TemplatePVCName is the name of the template PVC to copy base files from.
	// If empty, template sync is disabled.
	TemplatePVCName string
}

// dnsNameRegex validates DNS subdomain names per RFC 1123
var dnsNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// SanitizeName converts a string to a valid Kubernetes DNS name.
// It lowercases, replaces underscores with dashes, removes invalid characters,
// and ensures the name doesn't start or end with a dash.
func SanitizeName(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)
	// Replace underscores with dashes
	name = strings.ReplaceAll(name, "_", "-")
	// Remove any characters that aren't alphanumeric or dash
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	name = result.String()
	// Remove leading/trailing dashes and collapse multiple dashes
	name = strings.Trim(name, "-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return name
}

// ResourceName returns the prefixed resource name for a project.
// The project name is sanitized to ensure it's a valid Kubernetes name.
// Example: with prefix "kubetty-project-" and name "alpha", returns "kubetty-project-alpha"
func (c ResourceConfig) ResourceName(projectName string) string {
	sanitized := SanitizeName(projectName)
	return fmt.Sprintf("%s%s", c.Prefix, sanitized)
}

// ClusterRoleName returns the name for a cluster-scoped RBAC resource.
// Format: {prefix}{project-name}-{role}-{env}
// If Env is empty, it omits the environment suffix.
func (c ResourceConfig) ClusterRoleName(projectName, role string) string {
	baseName := c.ResourceName(projectName)
	if c.Env == "" {
		return fmt.Sprintf("%s-%s", baseName, role)
	}
	return fmt.Sprintf("%s-%s-%s", baseName, role, c.Env)
}

// buildImagePullSecrets converts a list of secret names to LocalObjectReferences.
func buildImagePullSecrets(secretNames []string) []corev1.LocalObjectReference {
	if len(secretNames) == 0 {
		return nil
	}
	refs := make([]corev1.LocalObjectReference, len(secretNames))
	for i, name := range secretNames {
		refs[i] = corev1.LocalObjectReference{Name: name}
	}
	return refs
}

// BuildPVC creates a PersistentVolumeClaim for project data storage.
// Name format: {prefix}{project-name}-data
func BuildPVC(p *projects.Project, cfg ResourceConfig) *corev1.PersistentVolumeClaim {
	storageQuantity := resource.MustParse(p.StorageSize)
	resourceName := fmt.Sprintf("%s-data", cfg.ResourceName(p.Name))

	return &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: cfg.Namespace,
			Labels:    projectLabels(p, cfg),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			StorageClassName: &p.StorageClass,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageQuantity,
				},
			},
		},
	}
}

// BuildServiceAccount creates a ServiceAccount for the project pod.
// Name format: {prefix}{project-name}-sa
func BuildServiceAccount(p *projects.Project, cfg ResourceConfig) *corev1.ServiceAccount {
	resourceName := fmt.Sprintf("%s-sa", cfg.ResourceName(p.Name))

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: cfg.Namespace,
			Labels:    projectLabels(p, cfg),
		},
	}
}

// BuildService creates a ClusterIP Service for the project pod.
// Name format: {prefix}{project-name}
func BuildService(p *projects.Project, cfg ResourceConfig) *corev1.Service {
	resourceName := cfg.ResourceName(p.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: cfg.Namespace,
			Labels:    projectLabels(p, cfg),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: projectSelectorLabels(p, cfg),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildDeployment creates a Deployment for the project pod.
// Name format: {prefix}{project-name}
func BuildDeployment(p *projects.Project, cfg ResourceConfig, envSecretName string) *appsv1.Deployment {
	replicas := int32(1)
	resourceName := cfg.ResourceName(p.Name)
	pvcName := fmt.Sprintf("%s-data", resourceName)
	saName := fmt.Sprintf("%s-sa", resourceName)

	// Parse resource quantities
	cpuRequest := resource.MustParse(p.CPURequest)
	cpuLimit := resource.MustParse(p.CPULimit)
	memoryRequest := resource.MustParse(p.MemoryRequest)
	memoryLimit := resource.MustParse(p.MemoryLimit)

	// Build containers
	containers := []corev1.Container{}
	volumes := []corev1.Volume{}

	// Data volume
	volumes = append(volumes, corev1.Volume{
		Name: "data",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
			},
		},
	})

	// Template volume (for base file sync) - only if configured
	if cfg.TemplatePVCName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "template",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: cfg.TemplatePVCName,
					ReadOnly:  true,
				},
			},
		})
	}

	// DinD sidecar if enabled
	if p.DinDEnabled {
		volumes = append(volumes, corev1.Volume{
			Name: "dind-sock",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})

		privileged := true
		containers = append(containers, corev1.Container{
			Name:  "docker",
			Image: "docker:24.0.5-dind",
			SecurityContext: &corev1.SecurityContext{
				Privileged: &privileged,
			},
			Env: []corev1.EnvVar{
				{Name: "DOCKER_TLS_CERTDIR", Value: ""},
			},
			Args: []string{
				"--host=unix:///var/run/docker/docker.sock",
				"--host=tcp://0.0.0.0:2375",
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "data", MountPath: "/var/lib/docker", SubPath: "var-lib-docker"},
				{Name: "dind-sock", MountPath: "/var/run/docker"},
			},
		})
	}

	// Main KubeTTY container
	kubettyEnv := []corev1.EnvVar{
		{Name: "POD_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		}},
		{Name: "KUBETTY_MODE", Value: "project"},
		{Name: "DEPLOYMENT_ID", Value: p.Name},
		{Name: "SESSION_ID", Value: p.SessionID.String()},
		{Name: "KUBETTY_USER", Value: p.UserName},
		{Name: "KUBETTY_PROJECT", Value: p.Name},
	}

	// Add DinD environment if enabled
	if p.DinDEnabled {
		kubettyEnv = append(kubettyEnv, corev1.EnvVar{
			Name:  "DOCKER_HOST",
			Value: "unix:///var/run/docker/docker.sock",
		})
	}

	// Add custom environment variables
	for k, v := range p.EnvVars {
		kubettyEnv = append(kubettyEnv, corev1.EnvVar{Name: k, Value: v})
	}

	kubettyVolumeMounts := []corev1.VolumeMount{
		{Name: "data", MountPath: "/opt", SubPath: "opt"},
		{Name: "data", MountPath: "/home", SubPath: "home"},
		{Name: "data", MountPath: "/usr/local/go", SubPath: "usr-local-go"},
	}

	if p.DinDEnabled {
		kubettyVolumeMounts = append(kubettyVolumeMounts, corev1.VolumeMount{
			Name:      "dind-sock",
			MountPath: "/var/run/docker",
		})
	}

	kubettyContainer := corev1.Container{
		Name:            "kubetty",
		Image:           fmt.Sprintf("%s:%s", p.ImageRepository, p.ImageTag),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             kubettyEnv,
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: 8080},
		},
		VolumeMounts: kubettyVolumeMounts,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    cpuRequest,
				corev1.ResourceMemory: memoryRequest,
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    cpuLimit,
				corev1.ResourceMemory: memoryLimit,
			},
		},
	}

	// Add envFrom if secret exists
	if envSecretName != "" {
		optional := true
		kubettyContainer.EnvFrom = []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: envSecretName},
					Optional:             &optional,
				},
			},
		}
	}

	containers = append(containers, kubettyContainer)

	// Init containers for permission setup
	userID := int64(1000)
	groupID := int64(1000)
	rootID := int64(0)

	initContainers := []corev1.Container{}

	// Template sync init container - runs FIRST if configured
	// Copies base files from template PVC to project PVC with retry logic
	if cfg.TemplatePVCName != "" {
		initContainers = append(initContainers, corev1.Container{
			Name:  "init-template",
			Image: "alpine:3.19",
			Command: []string{"/bin/sh", "-c", `
set -euo pipefail

MARKER_FILE="/data/.template-synced"
MAX_RETRIES=5
RETRY_DELAY=2

sync_with_retry() {
    attempt=1
    while [ $attempt -le $MAX_RETRIES ]; do
        echo "Syncing template files (attempt $attempt/$MAX_RETRIES)..."
        if cp -r /template/. /data/; then
            echo "Template sync completed successfully"
            # Create marker file with timestamp
            date -Iseconds > "$MARKER_FILE"
            return 0
        fi
        echo "Sync failed, retrying in ${RETRY_DELAY}s..."
        sleep $RETRY_DELAY
        RETRY_DELAY=$((RETRY_DELAY * 2))
        attempt=$((attempt + 1))
    done
    echo "ERROR: Template sync failed after $MAX_RETRIES attempts"
    return 1
}

# Only sync if marker file doesn't exist
if [ ! -f "$MARKER_FILE" ]; then
    sync_with_retry
else
    echo "Template already synced on $(cat $MARKER_FILE), skipping"
fi
`},
			SecurityContext: &corev1.SecurityContext{RunAsUser: &rootID},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "template", MountPath: "/template", ReadOnly: true},
				{Name: "data", MountPath: "/data"},
			},
		})
	}

	// Permission setup init container
	initContainers = append(initContainers, corev1.Container{
		Name:  "init-permissions",
		Image: "ubuntu:22.04",
		Command: []string{"/bin/bash", "-c", `
set -euo pipefail
if ! getent group mmattox >/dev/null 2>&1; then
  groupadd -g 1000 mmattox
fi
if ! id -u mmattox >/dev/null 2>&1; then
  useradd -u 1000 -g mmattox -M -s /usr/sbin/nologin mmattox
fi
mkdir -p /data/opt /data/home /data/usr-local-go /data/var-lib-docker
mkdir -p /data/home/mmattox
chown -R mmattox:mmattox /data/opt /data/home /data/usr-local-go
`},
		SecurityContext: &corev1.SecurityContext{RunAsUser: &rootID},
		VolumeMounts:    []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
	})

	// Home directory setup init container
	initContainers = append(initContainers, corev1.Container{
		Name:            "init-home",
		Image:           fmt.Sprintf("%s:%s", p.ImageRepository, p.ImageTag),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{"/bin/bash", "-c", `
set -euo pipefail
if [ ! -f /pvc-home/mmattox/.bash_profile ]; then
  echo "Copying home directory files from image to PVC..."
  cp -a /home/mmattox/. /pvc-home/mmattox/
  echo "Home directory files copied successfully"
else
  echo "Home directory already initialized, skipping copy"
fi
`},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  &userID,
			RunAsGroup: &groupID,
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: "/pvc-home", SubPath: "home"},
		},
	})

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: cfg.Namespace,
			Labels:    projectLabels(p, cfg),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: projectSelectorLabels(p, cfg),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: projectSelectorLabels(p, cfg),
					Annotations: map[string]string{
						"prometheus.io/scrape": "true",
						"prometheus.io/path":   "/metrics",
						"prometheus.io/port":   "8080",
					},
				},
				Spec: corev1.PodSpec{
					Hostname:           p.Name,
					ServiceAccountName: saName,
					SecurityContext: &corev1.PodSecurityContext{
						SupplementalGroups: []int64{0},
					},
					ImagePullSecrets: buildImagePullSecrets(cfg.ImagePullSecrets),
					InitContainers:   initContainers,
					Containers:       containers,
					Volumes:          volumes,
				},
			},
		},
	}
}

// BuildAdminClusterRole creates a ClusterRole with admin permissions.
// Name format: {prefix}{project-name}-admin-{env}
// This is cluster-scoped to allow cross-namespace access based on AdminNamespaces.
func BuildAdminClusterRole(p *projects.Project, cfg ResourceConfig) *rbacv1.ClusterRole {
	resourceName := cfg.ClusterRoleName(p.Name, "admin")

	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   resourceName,
			Labels: projectLabels(p, cfg),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"", "apps", "batch", "extensions", "networking.k8s.io"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
		},
	}
}

// BuildReadClusterRole creates a ClusterRole with read-only permissions.
// Name format: {prefix}{project-name}-read-{env}
// This is cluster-scoped to allow cross-namespace access based on ReadNamespaces.
func BuildReadClusterRole(p *projects.Project, cfg ResourceConfig) *rbacv1.ClusterRole {
	resourceName := cfg.ClusterRoleName(p.Name, "read")

	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   resourceName,
			Labels: projectLabels(p, cfg),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"", "apps", "batch", "extensions", "networking.k8s.io"},
				Resources: []string{"*"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}

// BuildAdminClusterRoleBinding creates a ClusterRoleBinding for admin access.
// Name format: {prefix}{project-name}-admin-{env}
// Binds the admin ClusterRole to the project's ServiceAccount.
func BuildAdminClusterRoleBinding(p *projects.Project, cfg ResourceConfig) *rbacv1.ClusterRoleBinding {
	resourceName := cfg.ClusterRoleName(p.Name, "admin")
	saName := fmt.Sprintf("%s-sa", cfg.ResourceName(p.Name))

	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   resourceName,
			Labels: projectLabels(p, cfg),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: cfg.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     resourceName,
		},
	}
}

// BuildReadClusterRoleBinding creates a ClusterRoleBinding for read-only access.
// Name format: {prefix}{project-name}-read-{env}
// Binds the read ClusterRole to the project's ServiceAccount.
func BuildReadClusterRoleBinding(p *projects.Project, cfg ResourceConfig) *rbacv1.ClusterRoleBinding {
	resourceName := cfg.ClusterRoleName(p.Name, "read")
	saName := fmt.Sprintf("%s-sa", cfg.ResourceName(p.Name))

	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   resourceName,
			Labels: projectLabels(p, cfg),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: cfg.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     resourceName,
		},
	}
}

// BuildEnvSecret creates a Secret for project-specific environment variables.
// Name format: {prefix}{project-name}-env
// This secret is mounted as envFrom in the project's deployment.
func BuildEnvSecret(p *projects.Project, cfg ResourceConfig, data map[string]string) *corev1.Secret {
	resourceName := fmt.Sprintf("%s-env", cfg.ResourceName(p.Name))

	// Convert string map to []byte map
	secretData := make(map[string][]byte)
	for k, v := range data {
		secretData[k] = []byte(v)
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: cfg.Namespace,
			Labels:    projectLabels(p, cfg),
		},
		Type: corev1.SecretTypeOpaque,
		Data: secretData,
	}
}

// EnvSecretName returns the name of the per-project environment secret.
// Name format: {prefix}{project-name}-env
func (c ResourceConfig) EnvSecretName(projectName string) string {
	return fmt.Sprintf("%s-env", c.ResourceName(projectName))
}

// projectLabels returns the standard labels for all project resources.
// Includes both Kubernetes-standard labels and KubeTTY-specific labels.
func projectLabels(p *projects.Project, cfg ResourceConfig) map[string]string {
	return map[string]string{
		labelApp:         "kubetty",
		labelInstance:    p.Name,
		labelManagedBy:   managedByValue,
		labelComponent:   "project",
		labelEnvironment: cfg.Env,
		labelProject:     p.Name,
	}
}

// projectSelectorLabels returns the minimal labels used for pod selection.
// These labels are used in Service selectors and Deployment label selectors.
func projectSelectorLabels(p *projects.Project, cfg ResourceConfig) map[string]string {
	return map[string]string{
		labelApp:      "kubetty",
		labelInstance: p.Name,
		labelProject:  p.Name,
	}
}
