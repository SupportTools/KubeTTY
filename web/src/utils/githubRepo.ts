export const GITHUB_REPO_PATTERN = /^[A-Za-z0-9][A-Za-z0-9-]{0,38}\/[A-Za-z0-9._-]{1,100}$/;

export function isValidGitHubRepo(repo: string): boolean {
  return repo === "" || GITHUB_REPO_PATTERN.test(repo);
}
