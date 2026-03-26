import { useState, useCallback } from 'react';
import { format } from 'date-fns';
import { CopyButton } from './CopyButton';
import { AnalyticsPanel } from './AnalyticsPanel';
import type { Link } from '../types';

interface LinkRowProps {
  link: Link;
  onDelete: (id: number) => Promise<void>;
  onToggleActive: (id: number, currentlyActive: boolean) => Promise<void>;
  onShowQR: (link: Link) => void;
  onAuthError: () => void;
}

export function LinkRow({
  link,
  onDelete,
  onToggleActive,
  onShowQR,
  onAuthError,
}: LinkRowProps) {
  const [deleting, setDeleting] = useState(false);
  const [toggling, setToggling] = useState(false);
  const [showAnalytics, setShowAnalytics] = useState(false);

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
    </>
  );
}
