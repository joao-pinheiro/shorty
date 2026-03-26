# Phase 10: Frontend Core Dashboard

## Summary

Build the core dashboard UI: shorten form with result display, link table with pagination/search/sort/filter, copy button, per-row actions (activate/deactivate, delete), and the `useLinks` hook for state management. This phase implements the primary user flow — creating short links and managing them in a table.

Spec references: S15.2 (dashboard layout, shorten form, link table, actions), S15.4 (API client methods used), S6.2 (create link), S6.4 (list links params/response), S6.6 (update link), S6.7 (delete link).

---

## Step 1: `useLinks` Hook (`frontend/src/hooks/useLinks.ts`)

Manages link list state, pagination, search, sort, and active filter. Provides methods for CRUD operations and refetching.

```typescript
import { useState, useEffect, useCallback } from 'react';
import { api, AuthError, ApiRequestError } from '../api/client';
import type { Link, ListParams } from '../types';

interface UseLinksOptions {
  onAuthError: () => void;
}

interface UseLinksReturn {
  links: Link[];
  total: number;
  page: number;
  perPage: number;
  loading: boolean;
  error: string | null;
  // State setters
  setPage: (page: number) => void;
  setPerPage: (perPage: number) => void;
  setSearch: (search: string) => void;
  setSort: (sort: ListParams['sort']) => void;
  setOrder: (order: ListParams['order']) => void;
  setActiveFilter: (active?: boolean) => void;
  setTagFilter: (tag?: string) => void;
  // Actions
  refresh: () => void;
  deleteLink: (id: number) => Promise<void>;
  toggleActive: (id: number, currentlyActive: boolean) => Promise<void>;
}

export function useLinks({ onAuthError }: UseLinksOptions): UseLinksReturn {
  const [links, setLinks] = useState<Link[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Query params
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(20);
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState<ListParams['sort']>('created_at');
  const [order, setOrder] = useState<ListParams['order']>('desc');
  const [activeFilter, setActiveFilter] = useState<boolean | undefined>(undefined);
  const [tagFilter, setTagFilter] = useState<string | undefined>(undefined);

  // Increment to force refetch
  const [refreshKey, setRefreshKey] = useState(0);

  const refresh = useCallback(() => {
    setRefreshKey((k) => k + 1);
  }, []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);

    const params: ListParams = {
      page,
      per_page: perPage,
      sort,
      order,
    };
    if (search) params.search = search;
    if (activeFilter !== undefined) params.active = activeFilter;
    if (tagFilter) params.tag = tagFilter;

    api.getLinks(params)
      .then((data) => {
        if (!cancelled) {
          setLinks(data.links);
          setTotal(data.total);
        }
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof AuthError) {
          onAuthError();
          return;
        }
        setError(err instanceof ApiRequestError ? err.message : 'Failed to load links');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => { cancelled = true; };
  }, [page, perPage, search, sort, order, activeFilter, tagFilter, refreshKey, onAuthError]);

  // Reset page to 1 when filters change
  const setSearchAndReset = useCallback((s: string) => {
    setSearch(s);
    setPage(1);
  }, []);

  const setActiveFilterAndReset = useCallback((a?: boolean) => {
    setActiveFilter(a);
    setPage(1);
  }, []);

  const setTagFilterAndReset = useCallback((t?: string) => {
    setTagFilter(t);
    setPage(1);
  }, []);

  const deleteLink = useCallback(async (id: number) => {
    try {
      await api.deleteLink(id);
      refresh();
    } catch (err) {
      if (err instanceof AuthError) { onAuthError(); return; }
      throw err;
    }
  }, [onAuthError, refresh]);

  const toggleActive = useCallback(async (id: number, currentlyActive: boolean) => {
    try {
      await api.updateLink(id, { is_active: !currentlyActive });
      refresh();
    } catch (err) {
      if (err instanceof AuthError) { onAuthError(); return; }
      throw err;
    }
  }, [onAuthError, refresh]);

  return {
    links, total, page, perPage, loading, error,
    setPage,
    setPerPage,
    setSearch: setSearchAndReset,
    setSort, setOrder,
    setActiveFilter: setActiveFilterAndReset,
    setTagFilter: setTagFilterAndReset,
    refresh, deleteLink, toggleActive,
  };
}
```

---

## Step 2: `CopyButton` Component (`frontend/src/components/CopyButton.tsx`)

```typescript
import { useState, useCallback } from 'react';

interface CopyButtonProps {
  text: string;
  className?: string;
}

export function CopyButton({ text, className = '' }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Fallback: select text in a temporary input
      const input = document.createElement('input');
      input.value = text;
      document.body.appendChild(input);
      input.select();
      document.execCommand('copy');
      document.body.removeChild(input);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  }, [text]);

  return (
    <button
      onClick={handleCopy}
      className={`text-sm px-2 py-1 rounded ${
        copied
          ? 'bg-green-100 text-green-700'
          : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
      } ${className}`}
      title="Copy to clipboard"
    >
      {copied ? 'Copied!' : 'Copy'}
    </button>
  );
}
```

---

## Step 3: `ShortenForm` Component (`frontend/src/components/ShortenForm.tsx`)

The form at the top of the dashboard. Fields: URL (required), custom code (optional), expiration dropdown (optional), tag multi-select (optional). Shows the created short URL with a copy button after success.

### Props and State

```typescript
import { useState, useCallback } from 'react';
import { api, AuthError, ApiRequestError } from '../api/client';
import { CopyButton } from './CopyButton';
import type { Link, TagWithCount } from '../types';

interface ShortenFormProps {
  onCreated: () => void;        // callback to refresh link list
  onAuthError: () => void;
  tags: TagWithCount[];         // available tags for multi-select
}

// Expiration presets (S15.2)
const EXPIRATION_OPTIONS = [
  { label: 'Never', value: 0 },
  { label: '1 hour', value: 3600 },
  { label: '1 day', value: 86400 },
  { label: '7 days', value: 604800 },
  { label: '30 days', value: 2592000 },
  { label: 'Custom', value: -1 },
] as const;
```

### Component

```typescript
export function ShortenForm({ onCreated, onAuthError, tags }: ShortenFormProps) {
  const [url, setUrl] = useState('');
  const [customCode, setCustomCode] = useState('');
  const [expirationPreset, setExpirationPreset] = useState(0);
  const [customExpiry, setCustomExpiry] = useState('');     // ISO date string for custom picker
  const [selectedTags, setSelectedTags] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState<Link | null>(null);

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setResult(null);

    const trimmedUrl = url.trim();
    if (!trimmedUrl) {
      setError('URL is required');
      return;
    }

    let expiresIn: number | undefined;
    if (expirationPreset === -1) {
      // Custom date: compute seconds from now
      if (!customExpiry) {
        setError('Please select an expiration date');
        return;
      }
      const expiresAt = new Date(customExpiry);
      const secondsFromNow = Math.floor((expiresAt.getTime() - Date.now()) / 1000);
      if (secondsFromNow <= 0) {
        setError('Expiration date must be in the future');
        return;
      }
      expiresIn = secondsFromNow;
    } else if (expirationPreset > 0) {
      expiresIn = expirationPreset;
    }

    setLoading(true);
    try {
      const link = await api.createLink(
        trimmedUrl,
        customCode.trim() || undefined,
        expiresIn,
        selectedTags.length > 0 ? selectedTags : undefined,
      );
      setResult(link);
      setUrl('');
      setCustomCode('');
      setExpirationPreset(0);
      setSelectedTags([]);
      onCreated();
    } catch (err) {
      if (err instanceof AuthError) { onAuthError(); return; }
      if (err instanceof ApiRequestError) {
        setError(err.message);
        return;
      }
      setError('An unexpected error occurred');
    } finally {
      setLoading(false);
    }
  }, [url, customCode, expirationPreset, customExpiry, selectedTags, onCreated, onAuthError]);

  const handleTagToggle = useCallback((tagName: string) => {
    setSelectedTags((prev) =>
      prev.includes(tagName)
        ? prev.filter((t) => t !== tagName)
        : [...prev, tagName]
    );
  }, []);

  return (
    <div className="bg-white rounded-lg shadow p-6 mb-6">
      <form onSubmit={handleSubmit} className="space-y-4">
        {/* URL Input */}
        <div>
          <input
            type="text"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="Enter URL to shorten"
            className="w-full border rounded px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>

        {/* Optional fields row */}
        <div className="flex flex-wrap gap-4">
          {/* Custom code */}
          <input
            type="text"
            value={customCode}
            onChange={(e) => setCustomCode(e.target.value)}
            placeholder="Custom code (optional)"
            className="border rounded px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />

          {/* Expiration dropdown */}
          <select
            value={expirationPreset}
            onChange={(e) => setExpirationPreset(Number(e.target.value))}
            className="border rounded px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            {EXPIRATION_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>

          {/* Custom date picker (shown when "Custom" selected) */}
          {expirationPreset === -1 && (
            <input
              type="datetime-local"
              value={customExpiry}
              onChange={(e) => setCustomExpiry(e.target.value)}
              className="border rounded px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          )}
        </div>

        {/* Tag multi-select */}
        {tags.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {tags.map((tag) => (
              <button
                key={tag.id}
                type="button"
                onClick={() => handleTagToggle(tag.name)}
                className={`text-sm px-3 py-1 rounded-full border ${
                  selectedTags.includes(tag.name)
                    ? 'bg-blue-100 border-blue-300 text-blue-700'
                    : 'bg-gray-50 border-gray-200 text-gray-600 hover:bg-gray-100'
                }`}
              >
                {tag.name}
              </button>
            ))}
          </div>
        )}

        {/* Error message */}
        {error && <p className="text-sm text-red-600">{error}</p>}

        {/* Submit */}
        <button
          type="submit"
          disabled={loading}
          className="bg-blue-600 text-white px-6 py-2 rounded hover:bg-blue-700 disabled:opacity-50"
        >
          {loading ? 'Shortening...' : 'Shorten'}
        </button>
      </form>

      {/* Result display */}
      {result && (
        <div className="mt-4 p-4 bg-green-50 border border-green-200 rounded flex items-center gap-3">
          <a
            href={result.short_url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 hover:underline font-mono"
          >
            {result.short_url}
          </a>
          <CopyButton text={result.short_url} />
        </div>
      )}
    </div>
  );
}
```

---

## Step 4: `SearchBar` Component (`frontend/src/components/SearchBar.tsx`)

Controls above the link table: search input, sort dropdown, active/all filter.

```typescript
import { useState, useCallback } from 'react';
import type { ListParams } from '../types';

interface SearchBarProps {
  onSearch: (search: string) => void;
  onSortChange: (sort: ListParams['sort']) => void;
  onOrderChange: (order: ListParams['order']) => void;
  onActiveFilterChange: (active?: boolean) => void;
  sort: ListParams['sort'];
  order: ListParams['order'];
}

export function SearchBar({
  onSearch,
  onSortChange,
  onOrderChange,
  onActiveFilterChange,
  sort,
  order,
}: SearchBarProps) {
  const [searchInput, setSearchInput] = useState('');

  const handleSearchSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault();
    onSearch(searchInput);
  }, [searchInput, onSearch]);

  // Debounced search on input change (simple approach: search on Enter or blur)
  const handleSearchChange = useCallback((value: string) => {
    setSearchInput(value);
    // Also trigger search on clear
    if (value === '') {
      onSearch('');
    }
  }, [onSearch]);

  return (
    <div className="flex flex-wrap items-center gap-4 mb-4">
      {/* Search */}
      <form onSubmit={handleSearchSubmit} className="flex-1 min-w-[200px]">
        <input
          type="text"
          value={searchInput}
          onChange={(e) => handleSearchChange(e.target.value)}
          placeholder="Search by URL or code..."
          className="w-full border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </form>

      {/* Sort */}
      <select
        value={sort}
        onChange={(e) => onSortChange(e.target.value as ListParams['sort'])}
        className="border rounded px-3 py-2 text-sm"
      >
        <option value="created_at">Created</option>
        <option value="click_count">Clicks</option>
        <option value="expires_at">Expires</option>
      </select>

      {/* Order */}
      <button
        onClick={() => onOrderChange(order === 'desc' ? 'asc' : 'desc')}
        className="border rounded px-3 py-2 text-sm hover:bg-gray-50"
        title={`Sort ${order === 'desc' ? 'ascending' : 'descending'}`}
      >
        {order === 'desc' ? 'Newest first' : 'Oldest first'}
      </button>

      {/* Active filter */}
      <select
        onChange={(e) => {
          const v = e.target.value;
          onActiveFilterChange(v === '' ? undefined : v === 'true');
        }}
        className="border rounded px-3 py-2 text-sm"
        defaultValue=""
      >
        <option value="">All links</option>
        <option value="true">Active only</option>
        <option value="false">Inactive only</option>
      </select>
    </div>
  );
}
```

---

## Step 5: `LinkRow` Component (`frontend/src/components/LinkRow.tsx`)

Single table row for a link. Shows truncated URL, code, created date, click count, tags, status badge, and action buttons.

```typescript
import { useState, useCallback } from 'react';
import { format } from 'date-fns';
import { CopyButton } from './CopyButton';
import type { Link } from '../types';

interface LinkRowProps {
  link: Link;
  onDelete: (id: number) => Promise<void>;
  onToggleActive: (id: number, currentlyActive: boolean) => Promise<void>;
  onShowQR: (link: Link) => void;
  // Note: Analytics is handled via row-level state, not a parent callback.
  // AnalyticsPanel (Phase 11) will be rendered inline beneath the row when expanded.
}

export function LinkRow({
  link,
  onDelete,
  onToggleActive,
  onShowQR,
}: LinkRowProps) {
  const [deleting, setDeleting] = useState(false);
  const [toggling, setToggling] = useState(false);
  const [showAnalytics, setShowAnalytics] = useState(false);

  const handleDelete = useCallback(async () => {
    if (!confirm('Delete this link? This cannot be undone.')) return;
    setDeleting(true);
    try {
      await onDelete(link.id);
    } finally {
      setDeleting(false);
    }
  }, [link.id, onDelete]);

  const handleToggle = useCallback(async () => {
    setToggling(true);
    try {
      await onToggleActive(link.id, link.is_active);
    } finally {
      setToggling(false);
    }
  }, [link.id, link.is_active, onToggleActive]);

  // Truncate URL to 60 chars for display
  const truncatedUrl = link.original_url.length > 60
    ? link.original_url.slice(0, 57) + '...'
    : link.original_url;

  const isExpired = link.expires_at && new Date(link.expires_at) < new Date();

  return (
    <>
    <tr className="border-b hover:bg-gray-50">
      {/* Short URL */}
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <a
            href={link.short_url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 hover:underline font-mono text-sm"
          >
            {link.code}
          </a>
          <CopyButton text={link.short_url} />
        </div>
      </td>

      {/* Original URL */}
      <td className="px-4 py-3 text-sm text-gray-600 max-w-xs truncate" title={link.original_url}>
        {truncatedUrl}
      </td>

      {/* Created */}
      <td className="px-4 py-3 text-sm text-gray-500">
        {format(new Date(link.created_at), 'MMM d, yyyy')}
      </td>

      {/* Clicks */}
      <td className="px-4 py-3 text-sm text-gray-700 text-right">
        {link.click_count.toLocaleString()}
      </td>

      {/* Tags */}
      <td className="px-4 py-3">
        <div className="flex flex-wrap gap-1">
          {link.tags.map((tag) => (
            <span
              key={tag}
              className="text-xs bg-gray-100 text-gray-600 px-2 py-0.5 rounded-full"
            >
              {tag}
            </span>
          ))}
        </div>
      </td>

      {/* Status */}
      <td className="px-4 py-3">
        {link.is_active ? (
          isExpired ? (
            <span className="text-xs bg-yellow-100 text-yellow-700 px-2 py-1 rounded">
              Expired
            </span>
          ) : (
            <span className="text-xs bg-green-100 text-green-700 px-2 py-1 rounded">
              Active
            </span>
          )
        ) : (
          <span className="text-xs bg-red-100 text-red-700 px-2 py-1 rounded">
            Inactive
          </span>
        )}
      </td>

      {/* Actions */}
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <button
            onClick={() => onShowQR(link)}
            className="text-xs text-gray-500 hover:text-gray-700"
            title="QR Code"
          >
            QR
          </button>
          <button
            onClick={() => setShowAnalytics(!showAnalytics)}
            className="text-xs text-gray-500 hover:text-gray-700"
            title="Analytics"
          >
            Stats
          </button>
          <button
            onClick={handleToggle}
            disabled={toggling}
            className="text-xs text-gray-500 hover:text-gray-700 disabled:opacity-50"
          >
            {link.is_active ? 'Deactivate' : 'Activate'}
          </button>
          <button
            onClick={handleDelete}
            disabled={deleting}
            className="text-xs text-red-500 hover:text-red-700 disabled:opacity-50"
          >
            Delete
          </button>
        </div>
      </td>
    </tr>
    {showAnalytics && (
      <tr>
        <td colSpan={7} className="px-4 py-3 bg-gray-50">
          {/* AnalyticsPanel (Phase 11) rendered inline when expanded */}
          <AnalyticsPanel linkId={link.id} />
        </td>
      </tr>
    )}
    </>
  );
}
```

> **Note**: The component's top-level return must wrap both `<tr>` elements in a `<React.Fragment>` (`<>...</>`).

---

## Step 6: `LinkTable` Component (`frontend/src/components/LinkTable.tsx`)

Renders the table of links with pagination controls.

```typescript
import { LinkRow } from './LinkRow';
import type { Link } from '../types';

interface LinkTableProps {
  links: Link[];
  total: number;
  page: number;
  perPage: number;
  loading: boolean;
  onPageChange: (page: number) => void;
  onDelete: (id: number) => Promise<void>;
  onToggleActive: (id: number, currentlyActive: boolean) => Promise<void>;
  onShowQR: (link: Link) => void;
  // Note: onShowAnalytics removed — LinkRow manages its own expanded state
  // and renders AnalyticsPanel (Phase 11) inline beneath the row.
}

export function LinkTable({
  links,
  total,
  page,
  perPage,
  loading,
  onPageChange,
  onDelete,
  onToggleActive,
  onShowQR,
}: LinkTableProps) {
  const totalPages = Math.ceil(total / perPage);

  if (loading) {
    return <div className="text-center py-8 text-gray-500">Loading...</div>;
  }

  if (links.length === 0) {
    return (
      <div className="text-center py-8 text-gray-500">
        No links found. Create one above.
      </div>
    );
  }

  return (
    <div>
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="border-b bg-gray-50 text-left text-sm text-gray-500">
              <th className="px-4 py-3 font-medium">Short URL</th>
              <th className="px-4 py-3 font-medium">Original URL</th>
              <th className="px-4 py-3 font-medium">Created</th>
              <th className="px-4 py-3 font-medium text-right">Clicks</th>
              <th className="px-4 py-3 font-medium">Tags</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Actions</th>
            </tr>
          </thead>
          <tbody>
            {links.map((link) => (
              <LinkRow
                key={link.id}
                link={link}
                onDelete={onDelete}
                onToggleActive={onToggleActive}
                onShowQR={onShowQR}
              />
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between mt-4 px-4">
          <span className="text-sm text-gray-500">
            Showing {(page - 1) * perPage + 1}–{Math.min(page * perPage, total)} of {total}
          </span>
          <div className="flex gap-2">
            <button
              onClick={() => onPageChange(page - 1)}
              disabled={page <= 1}
              className="px-3 py-1 text-sm border rounded hover:bg-gray-50 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              Previous
            </button>
            {/* Page numbers — show up to 5 around current */}
            {Array.from({ length: Math.min(totalPages, 5) }, (_, i) => {
              let pageNum: number;
              if (totalPages <= 5) {
                pageNum = i + 1;
              } else if (page <= 3) {
                pageNum = i + 1;
              } else if (page >= totalPages - 2) {
                pageNum = totalPages - 4 + i;
              } else {
                pageNum = page - 2 + i;
              }
              return (
                <button
                  key={pageNum}
                  onClick={() => onPageChange(pageNum)}
                  className={`px-3 py-1 text-sm border rounded ${
                    pageNum === page
                      ? 'bg-blue-600 text-white border-blue-600'
                      : 'hover:bg-gray-50'
                  }`}
                >
                  {pageNum}
                </button>
              );
            })}
            <button
              onClick={() => onPageChange(page + 1)}
              disabled={page >= totalPages}
              className="px-3 py-1 text-sm border rounded hover:bg-gray-50 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
```

---

## Step 7: `Dashboard` Page (`frontend/src/pages/Dashboard.tsx`)

Assembles all components. The QR and Analytics callbacks will show modals/panels implemented in Phase 11 — for now, they are stubs.

```typescript
import { useState, useEffect, useCallback } from 'react';
import { ShortenForm } from '../components/ShortenForm';
import { SearchBar } from '../components/SearchBar';
import { LinkTable } from '../components/LinkTable';
import { useLinks } from '../hooks/useLinks';
import { api, AuthError } from '../api/client';
import type { Link, TagWithCount } from '../types';

interface DashboardProps {
  onAuthError: () => void;
}

export function Dashboard({ onAuthError }: DashboardProps) {
  const {
    links, total, page, perPage, loading, error,
    setPage, setSearch, setSort, setOrder, setActiveFilter, setTagFilter,
    refresh, deleteLink, toggleActive,
  } = useLinks({ onAuthError });

  const [tags, setTags] = useState<TagWithCount[]>([]);
  const [sort, setSortState] = useState<'created_at' | 'click_count' | 'expires_at'>('created_at');
  const [order, setOrderState] = useState<'asc' | 'desc'>('desc');

  // Load tags for the form's tag multi-select
  useEffect(() => {
    api.getTags()
      .then(setTags)
      .catch((err) => {
        if (err instanceof AuthError) onAuthError();
      });
  }, [onAuthError]);

  const handleSortChange = useCallback((s: 'created_at' | 'click_count' | 'expires_at') => {
    setSortState(s);
    setSort(s);
  }, [setSort]);

  const handleOrderChange = useCallback((o: 'asc' | 'desc') => {
    setOrderState(o);
    setOrder(o);
  }, [setOrder]);

  // Stub handlers for Phase 11 features
  const handleShowQR = useCallback((link: Link) => {
    // Phase 11: open QR modal
    console.log('QR for', link.code);
  }, []);

  return (
    <div>
      <ShortenForm
        onCreated={refresh}
        onAuthError={onAuthError}
        tags={tags}
      />

      <div className="bg-white rounded-lg shadow">
        <div className="p-4">
          <SearchBar
            onSearch={setSearch}
            onSortChange={handleSortChange}
            onOrderChange={handleOrderChange}
            onActiveFilterChange={setActiveFilter}
            sort={sort}
            order={order}
          />
        </div>

        {error && (
          <div className="px-4 pb-2 text-sm text-red-600">{error}</div>
        )}

        <LinkTable
          links={links}
          total={total}
          page={page}
          perPage={perPage}
          loading={loading}
          onPageChange={setPage}
          onDelete={deleteLink}
          onToggleActive={toggleActive}
          onShowQR={handleShowQR}
        />
      </div>
    </div>
  );
}
```

---

## Step 8: Error Handling

All components follow the pattern established in Phase 9:

1. Catch `AuthError` — call `onAuthError()` to trigger key prompt.
2. Catch `ApiRequestError` — display `err.message` to user (these are the API's `"error"` field values from S13).
3. Catch unknown errors — display generic message.

Specific UI error states:
- **ShortenForm**: Error displayed below the form fields (red text).
- **LinkTable**: Error displayed above the table.
- **Delete/Toggle**: Errors can be shown via a toast or inline. For simplicity, `window.alert` is acceptable in Phase 10; a proper toast system can be added in Phase 11.

---

## Step 9: Testing

### `useLinks` Hook Tests

Use a test wrapper with MSW to mock API responses:

```typescript
import { renderHook, act, waitFor } from '@testing-library/react';
import { useLinks } from './useLinks';

it('fetches links on mount', async () => {
  server.use(
    http.get('*/api/v1/links', () => {
      return HttpResponse.json({
        links: [{ id: 1, code: 'abc', /* ... */ }],
        total: 1, page: 1, per_page: 20,
      });
    }),
  );

  const { result } = renderHook(() => useLinks({ onAuthError: vi.fn() }));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.links).toHaveLength(1);
});

it('resets to page 1 when search changes', async () => {
  // ...setup...
  const { result } = renderHook(() => useLinks({ onAuthError: vi.fn() }));
  act(() => result.current.setPage(3));
  act(() => result.current.setSearch('test'));
  // Verify the API was called with page=1
});

it('calls onAuthError on 401', async () => {
  server.use(
    http.get('*/api/v1/links', () => {
      return HttpResponse.json({ error: 'unauthorized' }, { status: 401 });
    }),
  );

  const onAuthError = vi.fn();
  renderHook(() => useLinks({ onAuthError }));
  await waitFor(() => expect(onAuthError).toHaveBeenCalled());
});
```

### `ShortenForm` Component Tests

```typescript
it('creates a link and shows result', async () => {
  server.use(
    http.post('*/api/v1/links', () => {
      return HttpResponse.json({
        id: 1, code: 'abc123',
        short_url: 'http://localhost:8080/abc123',
        original_url: 'https://example.com',
        /* ...other fields... */
      }, { status: 201 });
    }),
  );

  const onCreated = vi.fn();
  render(<ShortenForm onCreated={onCreated} onAuthError={vi.fn()} tags={[]} />);

  await userEvent.type(screen.getByPlaceholderText('Enter URL to shorten'), 'https://example.com');
  await userEvent.click(screen.getByText('Shorten'));

  await waitFor(() => {
    expect(screen.getByText('http://localhost:8080/abc123')).toBeInTheDocument();
  });
  expect(onCreated).toHaveBeenCalled();
});

it('shows error from API', async () => {
  server.use(
    http.post('*/api/v1/links', () => {
      return HttpResponse.json(
        { error: 'invalid URL: must be http or https' },
        { status: 400 },
      );
    }),
  );

  render(<ShortenForm onCreated={vi.fn()} onAuthError={vi.fn()} tags={[]} />);
  await userEvent.type(screen.getByPlaceholderText('Enter URL to shorten'), 'not-a-url');
  await userEvent.click(screen.getByText('Shorten'));

  await waitFor(() => {
    expect(screen.getByText('invalid URL: must be http or https')).toBeInTheDocument();
  });
});
```

### `CopyButton` Tests

```typescript
it('copies text to clipboard and shows feedback', async () => {
  // Mock clipboard API
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.assign(navigator, { clipboard: { writeText } });

  render(<CopyButton text="http://localhost:8080/abc" />);
  await userEvent.click(screen.getByText('Copy'));

  expect(writeText).toHaveBeenCalledWith('http://localhost:8080/abc');
  expect(screen.getByText('Copied!')).toBeInTheDocument();
});
```

### `LinkTable` Tests

```typescript
it('renders links and pagination', () => {
  const links = [
    { id: 1, code: 'abc', short_url: 'http://localhost:8080/abc', original_url: 'https://example.com', created_at: '2026-03-26T10:00:00Z', expires_at: null, is_active: true, click_count: 5, updated_at: '2026-03-26T10:00:00Z', tags: ['test'] },
  ];

  render(
    <LinkTable
      links={links}
      total={25}
      page={1}
      perPage={20}
      loading={false}
      onPageChange={vi.fn()}
      onDelete={vi.fn()}
      onToggleActive={vi.fn()}
      onShowQR={vi.fn()}
    />,
  );

  expect(screen.getByText('abc')).toBeInTheDocument();
  expect(screen.getByText('Next')).toBeInTheDocument();
});

it('shows empty state when no links', () => {
  render(
    <LinkTable links={[]} total={0} page={1} perPage={20} loading={false}
      onPageChange={vi.fn()} onDelete={vi.fn()} onToggleActive={vi.fn()}
      onShowQR={vi.fn()} />,
  );

  expect(screen.getByText(/no links found/i)).toBeInTheDocument();
});
```

### Verification Commands

```bash
cd frontend && npm test
cd frontend && npx tsc --noEmit
cd frontend && npm run dev  # manual verification
```

Manual verification checklist:
1. Create a link via the form, see it in the table.
2. Copy short URL to clipboard.
3. Search by URL or code.
4. Change sort column and order.
5. Filter by active/inactive.
6. Paginate through results (create >20 links).
7. Deactivate a link, verify status badge changes.
8. Delete a link, verify it disappears.
