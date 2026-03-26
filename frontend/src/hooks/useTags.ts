import { useState, useEffect, useCallback } from 'react';
import { api, AuthError, ApiRequestError } from '../api/client';
import type { TagWithCount } from '../types';

export function useTags(onAuthError: () => void) {
  const [tags, setTags] = useState<TagWithCount[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchTags = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await api.getTags();
      setTags(data);
    } catch (err) {
      if (err instanceof AuthError) { onAuthError(); return; }
      setError(err instanceof ApiRequestError ? err.message : 'Failed to load tags');
    } finally {
      setLoading(false);
    }
  }, [onAuthError]);

  useEffect(() => {
    fetchTags();
  }, [fetchTags]);

  const createTag = useCallback(async (name: string): Promise<TagWithCount> => {
    try {
      const tag = await api.createTag(name);
      const tagWithCount: TagWithCount = { ...tag, link_count: 0 };
      setTags((prev) => [...prev, tagWithCount]);
      return tagWithCount;
    } catch (err) {
      if (err instanceof AuthError) { onAuthError(); }
      if (err instanceof ApiRequestError) {
        setError(err.message);
      }
      throw err;
    }
  }, [onAuthError]);

  const deleteTag = useCallback(async (id: number) => {
    try {
      await api.deleteTag(id);
      setTags((prev) => prev.filter((t) => t.id !== id));
    } catch (err) {
      if (err instanceof AuthError) { onAuthError(); }
      if (err instanceof ApiRequestError) {
        setError(err.message);
      }
      throw err;
    }
  }, [onAuthError]);

  return { tags, loading, error, createTag, deleteTag, refetch: fetchTags };
}
