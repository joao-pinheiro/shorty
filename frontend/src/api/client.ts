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

  async createBulkLinks(urls: BulkRequestItem[]): Promise<BulkResponse> {
    return this.request<BulkResponse>('/api/v1/links/bulk', {
      method: 'POST',
      body: JSON.stringify({ urls }),
    });
  }

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

  async getLink(id: number): Promise<Link> {
    return this.request<Link>(`/api/v1/links/${id}`);
  }

  async updateLink(id: number, patch: LinkPatch): Promise<Link> {
    return this.request<Link>(`/api/v1/links/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    });
  }

  async deleteLink(id: number): Promise<void> {
    return this.request<void>(`/api/v1/links/${id}`, {
      method: 'DELETE',
    });
  }

  async getAnalytics(id: number, period: string): Promise<Analytics> {
    return this.request<Analytics>(
      `/api/v1/links/${id}/analytics?period=${encodeURIComponent(period)}`,
    );
  }

  async getQRCodeBlob(id: number, size: number = 256): Promise<Blob> {
    const key = getApiKey();
    const headers: Record<string, string> = {};
    if (key) {
      headers['Authorization'] = `Bearer ${key}`;
    }
    const res = await fetch(`${BASE_URL}/api/v1/links/${id}/qr?size=${size}`, { headers });
    if (res.status === 401) {
      throw new AuthError('unauthorized');
    }
    if (!res.ok) {
      const data = await res.json();
      throw new ApiRequestError(data.error, res.status);
    }
    return res.blob();
  }

  getPublicQRCodeUrl(code: string, size: number = 256): string {
    return `${BASE_URL}/${code}/qr?size=${size}`;
  }

  async getTags(): Promise<TagWithCount[]> {
    const data = await this.request<{ tags: TagWithCount[] }>('/api/v1/tags');
    return data.tags;
  }

  async createTag(name: string): Promise<Tag> {
    return this.request<Tag>('/api/v1/tags', {
      method: 'POST',
      body: JSON.stringify({ name }),
    });
  }

  async deleteTag(id: number): Promise<void> {
    return this.request<void>(`/api/v1/tags/${id}`, {
      method: 'DELETE',
    });
  }
}

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
