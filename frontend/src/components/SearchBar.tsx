import { useState, useCallback } from 'react';

interface SearchBarProps {
  onSearch: (search: string) => void;
  onSortChange: (sort: 'created_at' | 'click_count' | 'expires_at') => void;
  onOrderChange: (order: 'asc' | 'desc') => void;
  onActiveFilterChange: (active?: boolean) => void;
  sort: 'created_at' | 'click_count' | 'expires_at';
  order: 'asc' | 'desc';
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

  const handleSearchChange = useCallback((value: string) => {
    setSearchInput(value);
    if (value === '') {
      onSearch('');
    }
  }, [onSearch]);

  return (
    <div className="flex flex-wrap items-center gap-4 mb-4">
      <form onSubmit={handleSearchSubmit} className="flex-1 min-w-[200px]">
        <input
          type="text"
          value={searchInput}
          onChange={(e) => handleSearchChange(e.target.value)}
          placeholder="Search by URL or code..."
          className="w-full border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </form>

      <select
        value={sort}
        onChange={(e) => onSortChange(e.target.value as 'created_at' | 'click_count' | 'expires_at')}
        className="border rounded px-3 py-2 text-sm"
      >
        <option value="created_at">Created</option>
        <option value="click_count">Clicks</option>
        <option value="expires_at">Expires</option>
      </select>

      <button
        onClick={() => onOrderChange(order === 'desc' ? 'asc' : 'desc')}
        className="border rounded px-3 py-2 text-sm hover:bg-gray-50"
        title={`Sort ${order === 'desc' ? 'ascending' : 'descending'}`}
      >
        {order === 'desc' ? 'Newest first' : 'Oldest first'}
      </button>

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
