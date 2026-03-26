import { useState } from 'react';
import { api, AuthError, ApiRequestError } from '../api/client';
import { CopyButton } from './CopyButton';
import type { BulkResultItem } from '../types';

interface BulkShortenModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccess: () => void;
  onAuthError: () => void;
}

export function BulkShortenModal({ isOpen, onClose, onSuccess, onAuthError }: BulkShortenModalProps) {
  const [input, setInput] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [results, setResults] = useState<BulkResultItem[] | null>(null);
  const [summary, setSummary] = useState<{ total: number; succeeded: number; failed: number } | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (!isOpen) return null;

  const lines = input.split('\n').map(l => l.trim()).filter(Boolean);
  const tooMany = lines.length > 50;

  const handleSubmit = async () => {
    if (lines.length === 0 || tooMany) return;
    setSubmitting(true);
    setError(null);

    try {
      const resp = await api.createBulkLinks(lines.map(url => ({ url })));
      setResults(resp.results);
      setSummary({ total: resp.total, succeeded: resp.succeeded, failed: resp.failed });
    } catch (err) {
      if (err instanceof AuthError) { onAuthError(); return; }
      if (err instanceof ApiRequestError) {
        setError(err.message);
      } else {
        setError('An unexpected error occurred');
      }
    } finally {
      setSubmitting(false);
    }
  };

  const handleClose = () => {
    if (results) onSuccess();
    setInput('');
    setResults(null);
    setSummary(null);
    setError(null);
    onClose();
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg shadow-xl w-full max-w-lg max-h-[80vh] flex flex-col">
        <div className="flex items-center justify-between p-4 border-b">
          <h2 className="text-lg font-semibold">Bulk Shorten URLs</h2>
          <button onClick={handleClose} className="text-gray-400 hover:text-gray-600">&times;</button>
        </div>

        <div className="p-4 flex-1 overflow-y-auto">
          {!results ? (
            <>
              <textarea
                value={input}
                onChange={(e) => setInput(e.target.value)}
                placeholder="Enter one URL per line (max 50)"
                rows={10}
                className="w-full border rounded p-2 font-mono text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <div className="flex items-center justify-between mt-2">
                <span className="text-sm text-gray-500">{lines.length} URLs</span>
                {tooMany && <span className="text-sm text-red-600">Maximum 50 URLs</span>}
              </div>
              {error && <p className="text-sm text-red-600 mt-2">{error}</p>}
            </>
          ) : (
            <>
              {summary && (
                <p className="text-sm mb-3">
                  Created {summary.succeeded} of {summary.total} links
                  {summary.failed > 0 && ` (${summary.failed} failed)`}
                </p>
              )}
              <div className="space-y-2">
                {results.map((r, i) => (
                  <div
                    key={i}
                    className={`flex items-center gap-2 p-2 rounded text-sm ${
                      r.ok ? 'bg-green-50' : 'bg-red-50'
                    }`}
                  >
                    {r.ok ? (
                      <>
                        <span className="text-green-600">OK</span>
                        <span className="font-mono text-sm flex-1">{r.link?.short_url}</span>
                        <CopyButton text={r.link?.short_url ?? ''} />
                      </>
                    ) : (
                      <>
                        <span className="text-red-600">ERR</span>
                        <span className="flex-1 text-red-700">{r.error}</span>
                      </>
                    )}
                  </div>
                ))}
              </div>
            </>
          )}
        </div>

        <div className="flex justify-end gap-2 p-4 border-t">
          {!results ? (
            <>
              <button
                onClick={handleClose}
                className="px-4 py-2 text-sm border rounded hover:bg-gray-50"
              >
                Cancel
              </button>
              <button
                onClick={handleSubmit}
                disabled={submitting || lines.length === 0 || tooMany}
                className="px-4 py-2 text-sm bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
              >
                {submitting ? 'Shortening...' : 'Shorten All'}
              </button>
            </>
          ) : (
            <button
              onClick={handleClose}
              className="px-4 py-2 text-sm bg-blue-600 text-white rounded hover:bg-blue-700"
            >
              Close
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
