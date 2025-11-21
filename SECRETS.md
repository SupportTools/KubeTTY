# KubeTTY Secret Management

This document describes how to manage sensitive credentials and API keys for KubeTTY deployments.

## Overview

KubeTTY requires several API keys and credentials to function properly. These secrets are injected into the pod as environment variables from Kubernetes Secrets, keeping them separate from the Docker image and git repository.

## Required Secrets

### GitHub Token (`GITHUB_TOKEN`)
- **Purpose:** Access GitHub repositories, create issues, and interact with GitHub API
- **Type:** Personal Access Token (classic) or Fine-grained token
- **Scopes needed:**
  - `repo` (full repository access)
  - `workflow` (if managing workflows)
- **How to generate:** https://github.com/settings/tokens

### OpenAI API Key (`OPENAI_API_KEY`)
- **Purpose:** Access OpenAI GPT models for AI assistance
- **Type:** OpenAI API key
- **Format:** Starts with `sk-proj-` or `sk-`
- **How to generate:** https://platform.openai.com/api-keys

### Google Cloud Project (`GOOGLE_CLOUD_PROJECT`)
- **Purpose:** Specify the Google Cloud project for GCP operations
- **Type:** GCP Project ID (string)
- **Note:** This is not strictly secret but treated as configuration
- **How to find:** https://console.cloud.google.com/

### Nexmonyx Access Key (`NEXMONYX_ACCESS_KEY`)
- **Purpose:** Authenticate with Nexmonyx services
- **Type:** Access key ID
- **How to generate:** Contact your Nexmonyx administrator

### Nexmonyx Access Secret (`NEXMONYX_ACCESS_SECRET`)
- **Purpose:** Secret key for Nexmonyx authentication
- **Type:** Secret access key
- **How to generate:** Contact your Nexmonyx administrator

### Anthropic Base URL (`ANTHROPIC_BASE_URL`) - Optional
- **Purpose:** Override the default Anthropic API endpoint
- **Type:** URL
- **Default:** Uses standard Anthropic API endpoint
- **Use case:** Internal proxy or custom endpoint
- **Example:** `http://172.25.1.66:8080`

## Creating the Kubernetes Secret

### Method 1: Using kubectl create secret (Recommended)

Create a Kubernetes secret with all required credentials:

```bash
kubectl create secret generic kubetty-api-keys \
  -n kubetty-beacon-support \
  --from-literal=github-token='YOUR_GITHUB_TOKEN_HERE' \
  --from-literal=openai-api-key='YOUR_OPENAI_KEY_HERE' \
  --from-literal=google-cloud-project='YOUR_GCP_PROJECT_ID' \
  --from-literal=nexmonyx-access-key='YOUR_NEXMONYX_KEY_HERE' \
  --from-literal=nexmonyx-access-secret='YOUR_NEXMONYX_SECRET_HERE' \
  --from-literal=anthropic-base-url='YOUR_ANTHROPIC_URL_HERE'
```

**Notes:**
- Replace `kubetty-beacon-support` with your namespace
- Replace all `YOUR_*_HERE` placeholders with actual values
- Remove the `--from-literal` lines for any keys you don't need
- The secret name (`kubetty-api-keys`) should match the value in `values.yaml`

### Method 2: Using a YAML file

Create a file `kubetty-secrets.yaml` (DO NOT commit this to git):

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kubetty-api-keys
  namespace: kubetty-beacon-support
type: Opaque
stringData:
  github-token: "YOUR_GITHUB_TOKEN_HERE"
  openai-api-key: "YOUR_OPENAI_KEY_HERE"
  google-cloud-project: "YOUR_GCP_PROJECT_ID"
  nexmonyx-access-key: "YOUR_NEXMONYX_KEY_HERE"
  nexmonyx-access-secret: "YOUR_NEXMONYX_SECRET_HERE"
  anthropic-base-url: "YOUR_ANTHROPIC_URL_HERE"
```

Apply the secret:

```bash
kubectl apply -f kubetty-secrets.yaml
```

**Security Warning:** Delete this file after creating the secret, or store it securely outside the git repository.

## Configuring Helm to Use Secrets

Update your Helm values file (or use `--set` flags) to reference the secret:

```yaml
apiSecrets:
  existingSecret: "kubetty-api-keys"
```

Or via command line:

```bash
helm upgrade --install kubetty-my-project ./deploy/helm \
  -n kubetty-my-project \
  -f deploy/helm/values.project-template.yaml \
  --set apiSecrets.existingSecret=kubetty-api-keys \
  --set env.sessionID="$(uuidgen)"
```

## Verifying Secret Configuration

### Check if secret exists
```bash
kubectl get secret kubetty-api-keys -n kubetty-beacon-support
```

### Verify secret contains expected keys
```bash
kubectl describe secret kubetty-api-keys -n kubetty-beacon-support
```

### View secret values (use carefully)
```bash
# View all keys and values
kubectl get secret kubetty-api-keys -n kubetty-beacon-support -o yaml

# View specific key (base64 encoded)
kubectl get secret kubetty-api-keys -n kubetty-beacon-support -o jsonpath='{.data.github-token}' | base64 -d
```

### Check pod environment variables
```bash
# List environment variables in running pod
kubectl exec -it -n kubetty-beacon-support deployment/kubetty-beacon-support -- env | grep -E 'GITHUB_TOKEN|OPENAI_API_KEY'

# Or use describe
kubectl describe pod -n kubetty-beacon-support -l app.kubernetes.io/name=kubetty | grep -A 20 "Environment:"
```

## Rotating Secrets

### Step 1: Generate new credentials
Generate new API keys/tokens from respective services.

### Step 2: Update the Kubernetes secret
```bash
kubectl create secret generic kubetty-api-keys \
  -n kubetty-beacon-support \
  --from-literal=github-token='NEW_GITHUB_TOKEN' \
  --from-literal=openai-api-key='NEW_OPENAI_KEY' \
  --from-literal=google-cloud-project='YOUR_GCP_PROJECT_ID' \
  --from-literal=nexmonyx-access-key='NEW_NEXMONYX_KEY' \
  --from-literal=nexmonyx-access-secret='NEW_NEXMONYX_SECRET' \
  --from-literal=anthropic-base-url='YOUR_ANTHROPIC_URL' \
  --dry-run=client -o yaml | kubectl apply -f -
```

### Step 3: Restart the deployment
```bash
kubectl rollout restart deployment/kubetty-beacon-support -n kubetty-beacon-support
```

### Step 4: Verify the rollout
```bash
kubectl rollout status deployment/kubetty-beacon-support -n kubetty-beacon-support
```

### Step 5: Revoke old credentials
Revoke the old API keys/tokens from respective services to complete the rotation.

## Security Best Practices

### 🚨 CRITICAL SECURITY WARNINGS

1. **NEVER commit secrets to git**
   - Secrets are in `.gitignore` - keep it that way
   - If secrets are accidentally committed, they must be considered compromised
   - Rotate all exposed credentials immediately
   - Use tools like `git-secrets` or `gitleaks` to prevent accidental commits

2. **Rotate exposed credentials immediately**
   - If you've previously stored secrets in `.bash_profile` or other files, those credentials are exposed
   - Generate new tokens/keys from all services
   - Update Kubernetes secrets with new values
   - Revoke the old credentials

3. **Limit secret access**
   - Use Kubernetes RBAC to restrict who can read secrets
   - Only grant `get` permissions on secrets to necessary service accounts
   - Audit who has access to secrets regularly

4. **Use namespace isolation**
   - Deploy KubeTTY to dedicated namespaces
   - Don't share secrets across namespaces unless necessary
   - Use separate secrets for dev/staging/production

5. **Enable etcd encryption at rest**
   - Ensure your Kubernetes cluster has etcd encryption enabled
   - Secrets are base64-encoded by default, NOT encrypted
   - Contact your cluster administrator to enable encryption

6. **Monitor and audit**
   - Enable Kubernetes audit logging
   - Monitor secret access patterns
   - Set up alerts for unusual secret access

### Principle of Least Privilege

Only include the API keys that your specific deployment needs:

- **GitHub Token:** Only if you need GitHub integration
- **OpenAI Key:** Only if using OpenAI models
- **GCP Project:** Only if using Google Cloud services
- **Nexmonyx Keys:** Only if using Nexmonyx services
- **Anthropic URL:** Only if using a custom Anthropic endpoint

## Multi-Environment Setup

For multiple environments (dev, staging, production), create separate secrets in each namespace:

```bash
# Development namespace
kubectl create secret generic kubetty-api-keys \
  -n kubetty-dev \
  --from-literal=github-token='DEV_GITHUB_TOKEN' \
  ...

# Staging namespace
kubectl create secret generic kubetty-api-keys \
  -n kubetty-staging \
  --from-literal=github-token='STAGING_GITHUB_TOKEN' \
  ...

# Production namespace
kubectl create secret generic kubetty-api-keys \
  -n kubetty-production \
  --from-literal=github-token='PROD_GITHUB_TOKEN' \
  ...
```

## Troubleshooting

### Secret not found error
**Error:** `Error: secret "kubetty-api-keys" not found`

**Solution:** Create the secret before deploying:
```bash
kubectl create secret generic kubetty-api-keys -n YOUR_NAMESPACE --from-literal=github-token='...'
```

### Missing environment variables in pod
**Symptom:** Commands fail with "authentication failed" or "missing API key"

**Check:**
1. Verify secret exists: `kubectl get secret kubetty-api-keys -n NAMESPACE`
2. Check secret has correct keys: `kubectl describe secret kubetty-api-keys -n NAMESPACE`
3. Verify values.yaml references the secret: `apiSecrets.existingSecret: "kubetty-api-keys"`
4. Check pod environment: `kubectl exec -it POD_NAME -- env | grep TOKEN`

### Wrong secret values
**Symptom:** Authentication works but with wrong account/project

**Solution:** Update the secret values and restart the pod:
```bash
kubectl create secret generic kubetty-api-keys ... --dry-run=client -o yaml | kubectl apply -f -
kubectl rollout restart deployment/kubetty-DEPLOYMENT -n NAMESPACE
```

## Alternative Secret Management Solutions

For production environments or advanced use cases, consider:

### Sealed Secrets
- Encrypt secrets that can be safely committed to git
- Requires `sealed-secrets` controller in cluster
- Good for GitOps workflows
- https://github.com/bitnami-labs/sealed-secrets

### External Secrets Operator (ESO)
- Sync secrets from external sources (AWS Secrets Manager, HashiCorp Vault, etc.)
- Automatic rotation support
- Centralized secret management
- https://external-secrets.io/

### HashiCorp Vault
- Full-featured secret management platform
- Dynamic secrets with automatic rotation
- Detailed audit logs
- Requires Vault infrastructure
- https://www.vaultproject.io/

### SOPS (Secrets OPerationS)
- Encrypt individual values in YAML files
- Can be committed to git safely
- Integrates with cloud KMS services
- https://github.com/mozilla/sops

## Migration from .bash_profile

If you previously used `.bash_profile` with embedded secrets:

1. **Create the Kubernetes secret** with your credentials (as described above)
2. **Deploy with Helm** referencing the secret
3. **Verify** the pod has environment variables set
4. **Test** that services work with injected credentials
5. **Rotate all credentials** that were in the old .bash_profile
6. **Delete** any local copies of .bash_profile with secrets

The new sanitized `.bash_profile` is included in the Docker image but contains no secrets - all sensitive values come from Kubernetes.

## Support

If you encounter issues with secret management:

1. Check this documentation thoroughly
2. Verify all steps were followed correctly
3. Check Kubernetes pod logs: `kubectl logs -n NAMESPACE POD_NAME`
4. Consult your cluster administrator for cluster-specific secret policies
5. Review Kubernetes RBAC permissions

## Additional Resources

- [Kubernetes Secrets Documentation](https://kubernetes.io/docs/concepts/configuration/secret/)
- [Managing Secrets using kubectl](https://kubernetes.io/docs/tasks/configmap-secret/managing-secret-using-kubectl/)
- [Encrypting Secret Data at Rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
- [Good practices for Kubernetes Secrets](https://kubernetes.io/docs/concepts/security/secrets-good-practices/)
