package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/KubeTTY/server/internal/projects"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrSecretExternallyManaged means ESO manages the project env Secret, so KubeTTY must not write it.
var ErrSecretExternallyManaged = errors.New("project env secret is managed by an ExternalSecret")

// ErrUnknownSecretKey means a request asked to keep a value the Secret does not have.
var ErrUnknownSecretKey = errors.New("no existing value to keep")

// ProjectSecrets describes a project env Secret without its values.
type ProjectSecrets struct {
	Keys              []string `json:"keys"`
	ExternallyManaged bool     `json:"externallyManaged"`
}

// ESO labels every Secret it manages, but leaves it unowned while the Vault entry is empty.
const esoManagedLabel = "reconcile.external-secrets.io/managed"

func managedByExternalSecret(s *corev1.Secret) bool {
	return s.Labels[esoManagedLabel] == "true"
}

// GetProjectSecrets returns the key names of the project env Secret, never its values.
func (c *Controller) GetProjectSecrets(ctx context.Context, p *projects.Project) (ProjectSecrets, error) {
	cfg := c.cfg.ResourceConfig
	secret, err := c.clientset.CoreV1().Secrets(cfg.Namespace).Get(ctx, cfg.EnvSecretName(p.Name), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ProjectSecrets{Keys: []string{}}, nil
	}
	if err != nil {
		return ProjectSecrets{}, fmt.Errorf("failed to get secret: %w", err)
	}

	keys := make([]string, 0, len(secret.Data))
	for k := range secret.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return ProjectSecrets{Keys: keys, ExternallyManaged: managedByExternalSecret(secret)}, nil
}

// UpdateProjectSecrets makes the env Secret hold exactly the given keys, then restarts the project.
// A nil value keeps that key's current value, so callers never need to read values back.
func (c *Controller) UpdateProjectSecrets(ctx context.Context, p *projects.Project, secrets map[string]*string) error {
	logger := log.WithField("project", p.Name)
	cfg := c.cfg.ResourceConfig
	client := c.clientset.CoreV1().Secrets(cfg.Namespace)

	existing, err := client.Get(ctx, cfg.EnvSecretName(p.Name), metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get secret: %w", err)
	}
	found := err == nil
	if found && managedByExternalSecret(existing) {
		return ErrSecretExternallyManaged
	}

	data := make(map[string][]byte, len(secrets))
	for k, v := range secrets {
		if v != nil {
			data[k] = []byte(*v)
			continue
		}
		var current []byte
		ok := false
		if found {
			current, ok = existing.Data[k]
		}
		if !ok {
			return fmt.Errorf("%w for key %q", ErrUnknownSecretKey, k)
		}
		data[k] = current
	}

	if found {
		existing.Data = data
		if _, err := client.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update secret: %w", err)
		}
		logger.Info("Updated project env secret")
	} else {
		newSecret := BuildEnvSecret(p, cfg, nil)
		newSecret.Data = data
		if _, err := client.Create(ctx, newSecret, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("failed to create secret: %w", err)
		}
		logger.Info("Created project env secret")
	}

	return c.RestartProject(ctx, p)
}

// deleteEnvSecret deletes the project env Secret unless ESO manages it; removing its ExternalSecret is ESO's job.
func (c *Controller) deleteEnvSecret(ctx context.Context, p *projects.Project, logger *logrus.Entry) {
	cfg := c.cfg.ResourceConfig
	client := c.clientset.CoreV1().Secrets(cfg.Namespace)
	name := cfg.EnvSecretName(p.Name)

	secret, err := client.Get(ctx, name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		return
	case err != nil:
		logger.WithError(err).Warn("Failed to get env secret; not deleting it")
		return
	case managedByExternalSecret(secret):
		logger.Info("Keeping env secret: an ExternalSecret manages it")
		return
	}

	if err := client.Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		logger.WithError(err).Warn("Failed to delete env secret")
	}
}
