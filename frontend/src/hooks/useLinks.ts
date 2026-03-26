import { useState, useEffect, useCallback } from 'react';
import { api, AuthError, ApiRequestError } from '../api/client';
import type { Link, ListParams } from '../types';

interface UseLinksOptions {
  onAuthError: () => void;
}

export function useLinks({ onAuthError }: UseLinksOptions) {
  const [links, setLinks] = useState<Link[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(20);
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState<ListParams['sort']>('created_at');
  const [order, setOrder] = useState<ListParams['order']>('desc');
  const [activeFilter, setActiveFilter] = useState<boolean | undefined>(undefined);
  const [tagFilter, setTagFilter] = useState<string | undefined>(undefined);

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
