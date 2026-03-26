# Phase 9: Frontend Scaffolding

## Summary

Set up the React/TypeScript frontend project with Vite, Tailwind CSS, React Router, typed API client, and API key handling. This phase produces a working development environment that can communicate with the backend, prompt for the API key on 401 responses, and persist it in localStorage. No UI components beyond the key prompt and a placeholder dashboard page.

Spec references: S15.1 (tech stack), S15.3 (API key handling), S15.4 (API client), S15.5 (production serving, Vite config), S2 (project structure).

---

## Step 1: Initialize Vite Project

```bash
cd /data/shorty
npm create vite@latest frontend -- --template react-ts
cd frontend
npm install
```

### Install Dependencies (S15.1)

```bash
npm install react-router-dom date-fns recharts
npm install -D tailwindcss @tailwindcss/vite
```

---

## Step 2: Configure Tailwind CSS

Create `frontend/src/index.css`:

```css
@import "tailwindcss";
```

Add the Tailwind Vite plugin to `frontend/vite.config.ts`:

```typescript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
  ],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
```

The proxy configuration routes `/api` requests to the Go backend during development.

---

## Step 3: TypeScript Types (`frontend/src/types.ts`)

Define types matching all API response shapes:

```typescript
// Link object — returned by create, get, list, update (S6.2, S6.4, S6.5, S6.6)
export interface Link {
  id: number;
  code: string;
  short_url: string;
  original_url: string;
  created_at: string;   // ISO 8601
  expires_at: string | null;
  is_active: boolean;
  click_count: number;
  updated_at: string;
  tags: string[];
}

// Paginated links response (S6.4)
export interface PaginatedLinks {
  links: Link[];
  total: number;
  page: number;
  per_page: number;
}

// List params for GET /api/v1/links (S6.4)
export interface ListParams {
  page?: number;
  per_page?: number;
  search?: string;
  sort?: 'created_at' | 'click_count' | 'expires_at';
  order?: 'asc' | 'desc';
  active?: boolean;
  tag?: string;
}

// Link patch for PATCH /api/v1/links/:id (S6.6)
export interface LinkPatch {
  is_active?: boolean;
  expires_at?: string | null;
  tags?: string[];
}

// Bulk create types (S6.3)
export interface BulkRequestItem {
  url: string;
  custom_code?: string;
  expires_in?: number;
  tags?: string[];
}

export interface BulkResultItem {
  ok: boolean;
  link?: Link;
  error?: string;
  index?: number;
}

export interface BulkResponse {
  total: number;
  succeeded: number;
  failed: number;
  results: BulkResultItem[];
}

// Analytics (S6.8)
export interface DayCount {
  date: string;   // "2026-03-25"
  count: number;
}

export interface HourCount {
  hour: string;   // "2026-03-26T14:00:00Z"
  count: number;
}

export interface Analytics {
  link_id: number;
  total_clicks: number;
  period_clicks: number;
  clicks_by_day?: DayCount[];
  clicks_by_hour?: HourCount[];
}

// Tags (S6.10, S6.11)
export interface Tag {
  id: number;
  name: string;
  created_at: string;
}

export interface TagWithCount extends Tag {
  link_count: number;
}

// Error response (S13)
export interface ApiError {
  error: string;
  retry_after?: number;
}
```

---

## Step 4: API Client (`frontend/src/api/client.ts`)

```typescript
import type {
  Link, PaginatedLinks, ListParams, LinkPatch,
  BulkRequestItem, BulkResponse,
  Analytics, Tag, TagWithCount, ApiError,
} from '../types';

const BASE_URL = import.meta.env.VITE_API_URL || '';

function getApiKey(): string | null {
  return localStorage.getItem('shorty_api_key');
}

export function setApiKey(key: string): void {
  localStorage.setItem('shorty_api_key', key);
}

export function clearApiKey(): void {
  localStorage.removeItem('shorty_api_key');
}

export function hasApiKey(): boolean {
  return !!getApiKey();
}

class ApiClient {
  private async request<T>(
    path: string,
    options: RequestInit = {},
  ): Promise<T> {
    const key = getApiKey();
    const headers: Record<string, string> = {
      ...(options.headers as Record<string, string> || {}),
    };
    if (key) {
      headers['Authorization'] = `Bearer ${key}`;
    }
    if (options.body && typeof options.body === 'string') {
      headers['Content-Type'] = 'application/json';
    }

    const response = await fetch(`${BASE_URL}${path}`, {
      ...options,
      headers,
    });

    if (response.status === 401) {
      // Signal to the app that auth is needed
      throw new AuthError('unauthorized');
    }

    if (response.status === 204) {
      return undefined as T;
    }

    const data = await response.json();

    if (!response.ok) {
      const err = data as ApiError;
      throw new ApiRequestError(err.error, response.status, err.retry_after);
    }

    return data as T;
  }

  // Links (S6.2)
  async createLink(
    url: string,
    customCode?: string,
    expiresIn?: number,
    tags?: string[],
  ): Promise<Link> {
    const body: Record<string, unknown> = { url };
    if (customCode) body.custom_code = customCode;
    if (expiresIn !== undefined) body.expires_in = expiresIn;
    if (tags && tags.length > 0) body.tags = tags;

    return this.request<Link>('/api/v1/links', {
      method: 'POST',
      body: JSON.stringify(body),
    });
  }

  // Bulk (S6.3)
  async createBulkLinks(urls: BulkRequestItem[]): Promise<BulkResponse> {
    return this.request<BulkResponse>('/api/v1/links/bulk', {
      method: 'POST',
      body: JSON.stringify({ urls }),
    });
  }

  // List (S6.4)
  async getLinks(params: ListParams = {}): Promise<PaginatedLinks> {
    const searchParams = new URLSearchParams();
    if (params.page) searchParams.set('page', String(params.page));
    if (params.per_page) searchParams.set('per_page', String(params.per_page));
    if (params.search) searchParams.set('search', params.search);
    if (params.sort) searchParams.set('sort', params.sort);
    if (params.order) searchParams.set('order', params.order);
    if (params.active !== undefined) searchParams.set('active', String(params.active));
    if (params.tag) searchParams.set('tag', params.tag);

    const qs = searchParams.toString();
    return this.request<PaginatedLinks>(`/api/v1/links${qs ? '?' + qs : ''}`);
  }

  // Get single (S6.5)
  async getLink(id: number): Promise<Link> {
    return this.request<Link>(`/api/v1/links/${id}`);
  }

  // Update (S6.6)
  async updateLink(id: number, patch: LinkPatch): Promise<Link> {
    return this.request<Link>(`/api/v1/links/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    });
  }

  // Delete (S6.7)
  async deleteLink(id: number): Promise<void> {
    return this.request<void>(`/api/v1/links/${id}`, {
      method: 'DELETE',
    });
  }

  // Analytics (S6.8)
  async getAnalytics(id: number, period: string): Promise<Analytics> {
    return this.request<Analytics>(
      `/api/v1/links/${id}/analytics?period=${encodeURIComponent(period)}`,
    );
  }

  // QR Code URL (S6.9) — returns URL string, not a fetch
  getQRCodeUrl(id: number, size: number = 256): string {
    return `${BASE_URL}/api/v1/links/${id}/qr?size=${size}`;
  }

  // Tags (S6.10)
  async getTags(): Promise<TagWithCount[]> {
    const data = await this.request<{ tags: TagWithCount[] }>('/api/v1/tags');
    return data.tags;
  }

  // Create tag (S6.11)
  async createTag(name: string): Promise<Tag> {
    return this.request<Tag>('/api/v1/tags', {
      method: 'POST',
      body: JSON.stringify({ name }),
    });
  }

  // Delete tag (S6.12)
  async deleteTag(id: number): Promise<void> {
    return this.request<void>(`/api/v1/tags/${id}`, {
      method: 'DELETE',
    });
  }
}

// Custom error classes
export class AuthError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AuthError';
  }
}

export class ApiRequestError extends Error {
  status: number;
  retryAfter?: number;

  constructor(message: string, status: number, retryAfter?: number) {
    super(message);
    this.name = 'ApiRequestError';
    this.status = status;
    this.retryAfter = retryAfter;
  }
}

export const api = new ApiClient();
```

---

## Step 5: API Key Prompt Component

Create `frontend/src/components/ApiKeyPrompt.tsx`:

```typescript
import { useState } from 'react';
import { setApiKey } from '../api/client';

interface ApiKeyPromptProps {
  onKeySet: () => void;
}

export function ApiKeyPrompt({ onKeySet }: ApiKeyPromptProps) {
  const [key, setKey] = useState('');
  const [error, setError] = useState('');

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = key.trim();
    if (!trimmed) {
      setError('API key is required');
      return;
    }
    setApiKey(trimmed);
    setError('');
    onKeySet();
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg shadow-xl p-6 w-full max-w-md">
        <h2 className="text-lg font-semibold mb-4">Enter API Key</h2>
        <p className="text-sm text-gray-600 mb-4">
          An API key is required to access the dashboard.
        </p>
        <form onSubmit={handleSubmit}>
          <input
            type="password"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder="API key"
            className="w-full border rounded px-3 py-2 mb-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
            autoFocus
          />
          {error && (
            <p className="text-sm text-red-600 mb-2">{error}</p>
          )}
          <button
            type="submit"
            className="w-full bg-blue-600 text-white rounded px-4 py-2 hover:bg-blue-700"
          >
            Connect
          </button>
        </form>
      </div>
    </div>
  );
}
```

---

## Step 6: App Shell with Auth Handling (`frontend/src/App.tsx`)

```typescript
import { useState, useCallback, useEffect } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Dashboard } from './pages/Dashboard';
import { NotFound } from './pages/NotFound';
import { ApiKeyPrompt } from './components/ApiKeyPrompt';
import { hasApiKey, clearApiKey, api, AuthError } from './api/client';

export default function App() {
  const [needsKey, setNeedsKey] = useState(!hasApiKey());

  const handleKeySet = useCallback(() => {
    setNeedsKey(false);
  }, []);

  const handleAuthError = useCallback(() => {
    clearApiKey();
    setNeedsKey(true);
  }, []);

  // Verify key on mount by making a test request
  useEffect(() => {
    if (!needsKey) {
      api.getLinks({ page: 1, per_page: 1 }).catch((err) => {
        if (err instanceof AuthError) {
          handleAuthError();
        }
      });
    }
  }, [needsKey, handleAuthError]);

  return (
    <BrowserRouter>
      {needsKey && <ApiKeyPrompt onKeySet={handleKeySet} />}
      <div className="min-h-screen bg-gray-50">
        <header className="bg-white shadow-sm border-b">
          <div className="max-w-7xl mx-auto px-4 py-4 flex items-center justify-between">
            <h1 className="text-xl font-bold text-gray-900">Shorty</h1>
            {!needsKey && (
              <button
                onClick={handleAuthError}
                className="text-sm text-gray-500 hover:text-gray-700"
              >
                Change API Key
              </button>
            )}
          </div>
        </header>
        <main className="max-w-7xl mx-auto px-4 py-6">
          <Routes>
            <Route path="/" element={<Dashboard onAuthError={handleAuthError} />} />
            <Route path="*" element={<NotFound />} />
          </Routes>
        </main>
      </div>
    </BrowserRouter>
  );
}
```

The `onAuthError` callback is passed to components that make API calls. When they catch an `AuthError`, they call it to trigger the key prompt.

---

## Step 7: Placeholder Pages

### `frontend/src/pages/Dashboard.tsx`

```typescript
interface DashboardProps {
  onAuthError: () => void;
}

export function Dashboard({ onAuthError }: DashboardProps) {
  return (
    <div>
      <p className="text-gray-600">Dashboard placeholder — implemented in Phase 10.</p>
    </div>
  );
}
```

### `frontend/src/pages/NotFound.tsx`

```typescript
import { Link } from 'react-router-dom';

export function NotFound() {
  return (
    <div className="text-center py-20">
      <h2 className="text-2xl font-bold text-gray-900 mb-2">404</h2>
      <p className="text-gray-600 mb-4">Page not found.</p>
      <Link to="/" className="text-blue-600 hover:text-blue-800">
        Go to Dashboard
      </Link>
    </div>
  );
}
```

---

## Step 8: Entry Point (`frontend/src/index.tsx`)

Rename `frontend/src/main.tsx` (Vite default) or verify it imports correctly:

```typescript
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';
import './index.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
```

Update `frontend/index.html` to reference the correct entry point if the filename differs from Vite's default.

---

## Step 9: Vite Environment Variable

The API client reads `VITE_API_URL`. During development, the Vite proxy handles `/api` requests, so this defaults to empty string (same origin). For production, the Go binary serves both API and frontend on the same origin, so it also works with empty string.

If someone runs the frontend against a different backend:
```bash
VITE_API_URL=https://my-shorty.example.com npm run dev
```

Add type declaration in `frontend/src/vite-env.d.ts`:

```typescript
/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_URL: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
```

---

## Step 10: Update Makefile

Add the frontend dev target (S17.1):

```makefile
dev-frontend:
	cd frontend && npm run dev
```

Verify these targets work:

```makefile
dev-backend:
	cd backend && go run ./cmd/shorty

dev-frontend:
	cd frontend && npm run dev
```

---

## Step 11: Error Handling Patterns

All API calls in components should follow this pattern:

```typescript
import { AuthError, ApiRequestError } from '../api/client';

try {
  const result = await api.someMethod();
  // handle success
} catch (err) {
  if (err instanceof AuthError) {
    onAuthError();
    return;
  }
  if (err instanceof ApiRequestError) {
    // Show err.message to user (this is the "error" field from the API)
    setError(err.message);
    return;
  }
  // Network error or other
  setError('An unexpected error occurred');
}
```

This pattern will be used by all components in Phases 10 and 11.

---

## Step 12: Testing

### Setup

Install test dependencies:

```bash
cd frontend
npm install -D vitest @testing-library/react @testing-library/jest-dom @testing-library/user-event jsdom msw
```

Configure Vitest in `frontend/vite.config.ts`:

```typescript
export default defineConfig({
  // ... existing config ...
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
  },
})
```

Create `frontend/src/test/setup.ts`:

```typescript
import '@testing-library/jest-dom';
```

Add test script to `frontend/package.json`:

```json
{
  "scripts": {
    "test": "vitest run",
    "test:watch": "vitest"
  }
}
```

### API Client Tests (`frontend/src/api/client.test.ts`)

```typescript
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { api, setApiKey, clearApiKey, hasApiKey, AuthError, ApiRequestError } from './client';

// Use MSW to mock fetch responses
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => { server.resetHandlers(); clearApiKey(); });
afterAll(() => server.close());

describe('API Client', () => {
  it('includes Authorization header when key is set', async () => {
    let capturedHeaders: Headers;
    server.use(
      http.get('*/api/v1/links', ({ request }) => {
        capturedHeaders = request.headers;
        return HttpResponse.json({ links: [], total: 0, page: 1, per_page: 20 });
      }),
    );

    setApiKey('test-key');
    await api.getLinks();
    expect(capturedHeaders!.get('Authorization')).toBe('Bearer test-key');
  });

  it('throws AuthError on 401', async () => {
    server.use(
      http.get('*/api/v1/links', () => {
        return HttpResponse.json({ error: 'invalid API key' }, { status: 401 });
      }),
    );

    await expect(api.getLinks()).rejects.toThrow(AuthError);
  });

  it('throws ApiRequestError with message on 400', async () => {
    server.use(
      http.post('*/api/v1/links', () => {
        return HttpResponse.json(
          { error: 'invalid URL: must be http or https' },
          { status: 400 },
        );
      }),
    );

    setApiKey('key');
    try {
      await api.createLink('not-a-url');
      expect.fail('should throw');
    } catch (err) {
      expect(err).toBeInstanceOf(ApiRequestError);
      expect((err as ApiRequestError).message).toBe('invalid URL: must be http or https');
      expect((err as ApiRequestError).status).toBe(400);
    }
  });
});

describe('API key storage', () => {
  it('persists in localStorage', () => {
    expect(hasApiKey()).toBe(false);
    setApiKey('my-key');
    expect(hasApiKey()).toBe(true);
    clearApiKey();
    expect(hasApiKey()).toBe(false);
  });
});
```

### ApiKeyPrompt Component Test

```typescript
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ApiKeyPrompt } from '../components/ApiKeyPrompt';
import { hasApiKey, clearApiKey } from '../api/client';

afterEach(() => clearApiKey());

it('stores key and calls onKeySet', async () => {
  const onKeySet = vi.fn();
  render(<ApiKeyPrompt onKeySet={onKeySet} />);

  await userEvent.type(screen.getByPlaceholderText('API key'), 'my-secret');
  await userEvent.click(screen.getByText('Connect'));

  expect(hasApiKey()).toBe(true);
  expect(onKeySet).toHaveBeenCalledOnce();
});

it('shows error when submitting empty key', async () => {
  render(<ApiKeyPrompt onKeySet={vi.fn()} />);
  await userEvent.click(screen.getByText('Connect'));
  expect(screen.getByText('API key is required')).toBeInTheDocument();
});
```

### Verification Commands

```bash
cd frontend && npm run dev          # Verify dev server starts
cd frontend && npm test             # Verify tests pass
cd frontend && npx tsc --noEmit     # Verify TypeScript compiles
```
