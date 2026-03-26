import { useState } from 'react';
import { format } from 'date-fns';
import { useTags } from '../hooks/useTags';

interface TagManagerProps {
  isOpen: boolean;
  onClose: () => void;
  onAuthError: () => void;
}

const TAG_REGEX = /^[a-zA-Z0-9_-]{1,50}$/;

export function TagManager({ isOpen, onClose, onAuthError }: TagManagerProps) {
  const { tags, loading, error, createTag, deleteTag } = useTags(onAuthError);
  const [newTagName, setNewTagName] = useState('');
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);

  if (!isOpen) return null;

  const isValid = TAG_REGEX.test(newTagName);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newTagName.trim() || !isValid) return;
    setCreating(true);
    setCreateError(null);
    try {
      await createTag(newTagName.trim());
      setNewTagName('');
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : 'Failed to create tag');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: number, name: string) => {
    if (!confirm(`Delete tag '${name}'? It will be removed from all links.`)) return;
    try {
      await deleteTag(id);
    } catch {
      // error is set in the hook
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg shadow-xl w-full max-w-md max-h-[80vh] flex flex-col">
        <div className="flex items-center justify-between p-4 border-b">
          <h2 className="text-lg font-semibold">Manage Tags</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600">&times;</button>
        </div>

        <div className="p-4">
          <form onSubmit={handleCreate} className="flex gap-2 mb-4">
            <input
              type="text"
              value={newTagName}
              onChange={(e) => { setNewTagName(e.target.value); setCreateError(null); }}
              placeholder="New tag name"
              className="flex-1 border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
            <button
              type="submit"
              disabled={creating || !newTagName.trim() || !isValid}
              className="px-4 py-2 text-sm bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
            >
              Add
            </button>
          </form>
          {newTagName && !isValid && (
            <p className="text-xs text-red-600 mb-2">Must be 1-50 alphanumeric, dash, or underscore</p>
          )}
          {createError && <p className="text-xs text-red-600 mb-2">{createError}</p>}
          {error && <p className="text-xs text-red-600 mb-2">{error}</p>}
        </div>

        <div className="flex-1 overflow-y-auto px-4 pb-4">
          {loading && <p className="text-sm text-gray-500">Loading...</p>}
          {!loading && tags.length === 0 && <p className="text-sm text-gray-500">No tags yet</p>}
          {tags.map((tag) => (
            <div key={tag.id} className="flex items-center justify-between py-2 border-b last:border-0">
              <div>
                <span className="font-medium text-sm">{tag.name}</span>
                <span className="text-xs text-gray-500 ml-2">{tag.link_count} links</span>
                <span className="text-xs text-gray-400 ml-2">
                  {format(new Date(tag.created_at), 'MMM d, yyyy')}
                </span>
              </div>
              <button
                onClick={() => handleDelete(tag.id, tag.name)}
                className="text-xs text-red-500 hover:text-red-700"
              >
                Delete
              </button>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
