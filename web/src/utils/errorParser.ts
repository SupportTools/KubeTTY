import type { ErrorResponse } from '../types';

/**
 * Parses an HTTP error response and extracts a user-friendly error message.
 *
 * This utility handles the new JSON error format introduced in Task #3787
 * while maintaining backward compatibility with plain text errors.
 *
 * @param response - The failed HTTP Response object
 * @returns A clean, user-friendly error message
 *
 * @example
 * ```typescript
 * const res = await fetch('/api/auth/login', { ... });
 * if (!res.ok) {
 *   const message = await parseErrorResponse(res);
 *   throw new Error(message);
 * }
 * ```
 */
export async function parseErrorResponse(response: Response): Promise<string> {
  try {
    const contentType = response.headers.get('content-type');

    // Try JSON first (new format from Task #3787)
    if (contentType?.startsWith('application/json')) {
      try {
        const data = await response.json();

        // Validate response structure before using it
        if (typeof data === 'object' && data !== null && typeof data.message === 'string') {
          const errorData = data as ErrorResponse;

          // Combine message with details if available (trim whitespace)
          if (errorData.details && errorData.details.trim()) {
            return `${errorData.message}: ${errorData.details}`;
          }

          return errorData.message;
        }

        // Fallback for malformed JSON structure
        return `Request failed with status ${response.status}`;
      } catch {
        // JSON parsing failed, return generic message
        return `Request failed with status ${response.status}`;
      }
    }

    // Fallback to plain text (backward compatibility)
    const text = await response.text();
    return text || `Request failed with status ${response.status}`;
  } catch (error) {
    // If parsing fails completely, return generic message
    return `Request failed with status ${response.status}`;
  }
}
