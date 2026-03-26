import { useState, useCallback } from 'react';
import { api, AuthError, ApiRequestError } from '../api/client';
import { CopyButton } from './CopyButton';
import type { Link, TagWithCount } from '../types';

interface ShortenFormProps {
  onCreated: () => void;
  onAuthError: () => void;
  tags: TagWithCount[];
}

const EXPIRATION_OPTIONS = [
  { label: 'Never', value: 0 },
  { label: '1 hour', value: 3600 },
  { label: '1 day', value: 86400 },
  { label: '7 days', value: 604800 },
  { label: '30 days', value: 2592000 },
  { label: 'Custom', value: -1 },
] as const;

export function ShortenForm({ onCreated, onAuthError, tags }: ShortenFormProps) {
  const [url, setUrl] = useState('');
  const [customCode, setCustomCode] = useState('');
  const [expirationPreset, setExpirationPreset] = useState(0);
  const [customExpiry, setCustomExpiry] = useState('');
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
      setCustomExpiry('');
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
        <div>
          <input
            type="text"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="Enter URL to shorten"
            className="w-full border rounded px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>

        <div className="flex flex-wrap gap-4">
          <input
            type="text"
            value={customCode}
            onChange={(e) => setCustomCode(e.target.value)}
            placeholder="Custom code (optional)"
            className="border rounded px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />

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

          {expirationPreset === -1 && (
            <input
              type="datetime-local"
              value={customExpiry}
              onChange={(e) => setCustomExpiry(e.target.value)}
              className="border rounded px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          )}
        </div>

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

        {error && <p className="text-sm text-red-600">{error}</p>}

        <button
          type="submit"
          disabled={loading}
          className="bg-blue-600 text-white px-6 py-2 rounded hover:bg-blue-700 disabled:opacity-50"
        >
          {loading ? 'Shortening...' : 'Shorten'}
        </button>
      </form>

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
