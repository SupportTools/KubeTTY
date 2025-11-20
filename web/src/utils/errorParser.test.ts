import { describe, it, expect, beforeEach } from 'vitest';
import { parseErrorResponse } from './errorParser';
import type { ErrorResponse } from '../types';

describe('parseErrorResponse', () => {
  describe('JSON error responses', () => {
    it('should parse valid JSON error response with message only', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 400,
          error: 'bad_request',
          message: 'Invalid input'
        }),
        {
          status: 400,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Invalid input');
    });

    it('should parse valid JSON error response with message and details', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 400,
          error: 'bad_request',
          message: 'Validation failed',
          details: 'Username is required'
        }),
        {
          status: 400,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Validation failed: Username is required');
    });

    it('should handle JSON response with missing message field', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 500,
          error: 'internal_error'
        }),
        {
          status: 500,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Request failed with status 500');
    });

    it('should handle malformed JSON', async () => {
      const mockResponse = new Response(
        '{invalid json',
        {
          status: 500,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Request failed with status 500');
    });

    it('should handle null JSON response', async () => {
      const mockResponse = new Response(
        'null',
        {
          status: 500,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Request failed with status 500');
    });

    it('should handle array JSON response', async () => {
      const mockResponse = new Response(
        '[]',
        {
          status: 500,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Request failed with status 500');
    });

    it('should handle JSON with non-string message field', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 400,
          error: 'bad_request',
          message: 123
        }),
        {
          status: 400,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Request failed with status 400');
    });

    it('should handle content-type with charset', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 401,
          error: 'unauthorized',
          message: 'Authentication required'
        }),
        {
          status: 401,
          headers: { 'content-type': 'application/json; charset=utf-8' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Authentication required');
    });
  });

  describe('Plain text error responses', () => {
    it('should parse plain text error response', async () => {
      const mockResponse = new Response(
        'Internal server error',
        {
          status: 500,
          headers: { 'content-type': 'text/plain' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Internal server error');
    });

    it('should handle empty plain text response', async () => {
      const mockResponse = new Response(
        '',
        {
          status: 500,
          headers: { 'content-type': 'text/plain' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Request failed with status 500');
    });

    it('should handle response without content-type header', async () => {
      const mockResponse = new Response(
        'Error occurred',
        {
          status: 500
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Error occurred');
    });
  });

  describe('Edge cases', () => {
    it('should handle 404 responses', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 404,
          error: 'not_found',
          message: 'Resource not found'
        }),
        {
          status: 404,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Resource not found');
    });

    it('should handle 401 responses', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 401,
          error: 'unauthorized',
          message: 'Invalid credentials'
        }),
        {
          status: 401,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Invalid credentials');
    });

    it('should handle 403 responses', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 403,
          error: 'forbidden',
          message: 'Access denied'
        }),
        {
          status: 403,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Access denied');
    });

    it('should handle responses with empty details', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 400,
          error: 'bad_request',
          message: 'Validation error',
          details: ''
        }),
        {
          status: 400,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      // Empty details should not append colon
      expect(result).toBe('Validation error');
    });

    it('should handle HTML error responses', async () => {
      const mockResponse = new Response(
        '<html><body>Error</body></html>',
        {
          status: 500,
          headers: { 'content-type': 'text/html' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('<html><body>Error</body></html>');
    });
  });

  describe('Real-world scenarios', () => {
    it('should handle login failure', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 401,
          error: 'invalid_credentials',
          message: 'Invalid username or password'
        }),
        {
          status: 401,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Invalid username or password');
    });

    it('should handle tab creation failure', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 400,
          error: 'invalid_project',
          message: 'Project not found',
          details: 'Project ID does not exist'
        }),
        {
          status: 400,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Project not found: Project ID does not exist');
    });

    it('should handle session logs fetch failure', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 404,
          error: 'session_not_found',
          message: 'Session does not exist'
        }),
        {
          status: 404,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Session does not exist');
    });

    it('should handle password change with wrong current password', async () => {
      const mockResponse = new Response(
        JSON.stringify({
          status: 400,
          error: 'invalid_password',
          message: 'Current password is incorrect'
        }),
        {
          status: 400,
          headers: { 'content-type': 'application/json' }
        }
      );

      const result = await parseErrorResponse(mockResponse);
      expect(result).toBe('Current password is incorrect');
    });
  });
});
