import { useState, useCallback } from 'react';
import { format } from 'date-fns';
import { CopyButton } from './CopyButton';
import { AnalyticsPanel } from './AnalyticsPanel';
import { api, AuthError } from '../api/client';
import type { Link, TagWithCount } from '../types';

interface LinkRowProps {
  link: Link;
  allTags: TagWithCount[];
  onDelete: (id: number) => Promise<void>;
  onToggleActive: (id: number, currentlyActive: boolean) => Promise<void>;
  onShowQR: (link: Link) => void;
  onAuthError: () => void;
  onTagsUpdated: () => void;
}

export function LinkRow({
  link,
  allTags,
  onDelete,
  onToggleActive,
  onShowQR,
  onAuthError,
  onTagsUpdated,
}: LinkRowProps) {
  const [deleting, setDeleting] = useState(false);
  const [toggling, setToggling] = useState(false);
  const [showAnalytics, setShowAnalytics] = useState(false);
  const [editingTags, setEditingTags] = useState(false);
  const [selectedTags, setSelectedTags] = useState<string[]>(link.tags);
  const [savingTags, setSavingTags] = useState(false);

  const handleDelete = useCallback(async () => {
    if (!confirm('Delete this link? This cannot be undone.')) return;
    setDeleting(true);
    try {
      await onDelete(link.id);
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete link');
    } finally {
      setDeleting(false);
    }
  }, [link.id, onDelete]);

  const handleToggle = useCallback(async () => {
    setToggling(true);
    try {
      await onToggleActive(link.id, link.is_active);
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to update link');
    } finally {
      setToggling(false);
    }
  }, [link.id, link.is_active, onToggleActive]);

  const handleEditTags = useCallback(() => {
    setSelectedTags(link.tags);
    setEditingTags(true);
  }, [link.tags]);

  const handleSaveTags = useCallback(async () => {
    setSavingTags(true);
    try {
      await api.updateLink(link.id, { tags: selectedTags });
      setEditingTags(false);
      onTagsUpdated();
    } catch (err) {
      if (err instanceof AuthError) { onAuthError(); return; }
      alert(err instanceof Error ? err.message : 'Failed to update tags');
    } finally {
      setSavingTags(false);
    }
  }, [link.id, selectedTags, onAuthError, onTagsUpdated]);

  const handleTagToggle = useCallback((tagName: string) => {
    setSelectedTags((prev) =>
      prev.includes(tagName) ? prev.filter((t) => t !== tagName) : [...prev, tagName]
    );
  }, []);

  const truncatedUrl = link.original_url.length > 60
    ? link.original_url.slice(0, 57) + '...'
    : link.original_url;

  const isExpired = link.expires_at && new Date(link.expires_at) < new Date();

  return (
    <>
      <tr className="border-b hover:bg-gray-50">
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

        <td className="px-4 py-3 text-sm text-gray-600 max-w-xs truncate" title={link.original_url}>
          {truncatedUrl}
        </td>

        <td className="px-4 py-3 text-sm text-gray-500">
          {format(new Date(link.created_at), 'MMM d, yyyy')}
        </td>

        <td className="px-4 py-3 text-sm text-gray-700 text-right">
          {link.click_count.toLocaleString()}
        </td>

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
              className={`text-xs ${showAnalytics ? 'text-blue-600' : 'text-gray-500 hover:text-gray-700'}`}
              title="Analytics"
            >
              Stats
            </button>
            <button
              onClick={handleEditTags}
              className={`text-xs ${editingTags ? 'text-blue-600' : 'text-gray-500 hover:text-gray-700'}`}
              title="Edit Tags"
            >
              Tags
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
          <td colSpan={7} className="px-4 py-3 bg-gray-50 border-b">
            <AnalyticsPanel linkId={link.id} onAuthError={onAuthError} />
          </td>
        </tr>
      )}
      {editingTags && (
        <tr>
          <td colSpan={7} className="px-4 py-3 bg-blue-50 border-b">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm text-gray-600 mr-1">Tags:</span>
              {allTags.length === 0 && (
                <span className="text-sm text-gray-400">No tags created yet. Use Tag Manager to create tags.</span>
              )}
              {allTags.map((tag) => (
                <button
                  key={tag.id}
                  type="button"
                  onClick={() => handleTagToggle(tag.name)}
                  className={`text-xs px-3 py-1 rounded-full border ${
                    selectedTags.includes(tag.name)
                      ? 'bg-blue-100 border-blue-300 text-blue-700'
                      : 'bg-white border-gray-200 text-gray-600 hover:bg-gray-50'
                  }`}
                >
                  {tag.name}
                </button>
              ))}
              <div className="ml-auto flex gap-2">
                <button
                  onClick={handleSaveTags}
                  disabled={savingTags}
                  className="text-xs px-3 py-1 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
                >
                  {savingTags ? 'Saving...' : 'Save'}
                </button>
                <button
                  onClick={() => setEditingTags(false)}
                  className="text-xs px-3 py-1 border rounded hover:bg-gray-50"
                >
                  Cancel
                </button>
              </div>
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
