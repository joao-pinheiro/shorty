import { useState, useCallback } from 'react';
import { ShortenForm } from '../components/ShortenForm';
import { SearchBar } from '../components/SearchBar';
import { LinkTable } from '../components/LinkTable';
import { TagFilter } from '../components/TagFilter';
import { BulkShortenModal } from '../components/BulkShortenModal';
import { QRCodeModal } from '../components/QRCodeModal';
import { TagManager } from '../components/TagManager';
import { useLinks } from '../hooks/useLinks';
import { useTags } from '../hooks/useTags';
import type { Link } from '../types';

interface DashboardProps {
  onAuthError: () => void;
}

export function Dashboard({ onAuthError }: DashboardProps) {
  const {
    links, total, page, perPage, loading, error,
    setPage, setSearch, setSort, setOrder, setActiveFilter, setTagFilter,
    refresh, deleteLink, toggleActive,
  } = useLinks({ onAuthError });

  const { tags, refetch: refetchTags } = useTags(onAuthError);

  const [sort, setSortState] = useState<'created_at' | 'click_count' | 'expires_at'>('created_at');
  const [order, setOrderState] = useState<'asc' | 'desc'>('desc');
  const [selectedTag, setSelectedTag] = useState<string | null>(null);

  const [bulkModalOpen, setBulkModalOpen] = useState(false);
  const [qrModal, setQrModal] = useState<{ linkId: number; code: string; shortUrl: string } | null>(null);
  const [tagManagerOpen, setTagManagerOpen] = useState(false);

  const handleSortChange = useCallback((s: 'created_at' | 'click_count' | 'expires_at') => {
    setSortState(s);
    setSort(s);
  }, [setSort]);

  const handleOrderChange = useCallback((o: 'asc' | 'desc') => {
    setOrderState(o);
    setOrder(o);
  }, [setOrder]);

  const handleTagFilterChange = useCallback((tag: string | null) => {
    setSelectedTag(tag);
    setTagFilter(tag ?? undefined);
  }, [setTagFilter]);

  const handleShowQR = useCallback((link: Link) => {
    setQrModal({ linkId: link.id, code: link.code, shortUrl: link.short_url });
  }, []);

  const handleCreated = useCallback(() => {
    refresh();
    refetchTags();
  }, [refresh, refetchTags]);

  return (
    <div>
      <ShortenForm
        onCreated={handleCreated}
        onAuthError={onAuthError}
        tags={tags}
      />

      <div className="bg-white rounded-lg shadow">
        <div className="p-4">
          <div className="flex flex-wrap items-center gap-4 mb-4">
            <div className="flex-1">
              <SearchBar
                onSearch={setSearch}
                onSortChange={handleSortChange}
                onOrderChange={handleOrderChange}
                onActiveFilterChange={setActiveFilter}
                sort={sort}
                order={order}
              />
            </div>
            <TagFilter
              tags={tags}
              selectedTag={selectedTag}
              onChange={handleTagFilterChange}
            />
            <button
              onClick={() => setBulkModalOpen(true)}
              className="px-3 py-2 text-sm border rounded hover:bg-gray-50"
            >
              Bulk Shorten
            </button>
            <button
              onClick={() => setTagManagerOpen(true)}
              className="px-3 py-2 text-sm border rounded hover:bg-gray-50"
            >
              Tags
            </button>
          </div>
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
          onAuthError={onAuthError}
        />
      </div>

      <BulkShortenModal
        isOpen={bulkModalOpen}
        onClose={() => setBulkModalOpen(false)}
        onSuccess={() => { refresh(); refetchTags(); }}
        onAuthError={onAuthError}
      />

      {qrModal && (
        <QRCodeModal
          isOpen={true}
          onClose={() => setQrModal(null)}
          linkId={qrModal.linkId}
          shortUrl={qrModal.shortUrl}
          code={qrModal.code}
          onAuthError={onAuthError}
        />
      )}

      <TagManager
        isOpen={tagManagerOpen}
        onClose={() => { setTagManagerOpen(false); refetchTags(); }}
        onAuthError={onAuthError}
      />
    </div>
  );
}
