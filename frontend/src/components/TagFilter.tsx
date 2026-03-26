import type { TagWithCount } from '../types';

interface TagFilterProps {
  tags: TagWithCount[];
  selectedTag: string | null;
  onChange: (tag: string | null) => void;
}

export function TagFilter({ tags, selectedTag, onChange }: TagFilterProps) {
  return (
    <select
      value={selectedTag ?? ''}
      onChange={(e) => onChange(e.target.value || null)}
      className="border rounded px-3 py-2 text-sm"
    >
      <option value="">All tags</option>
      {tags.map((tag) => (
        <option key={tag.id} value={tag.name}>
          {tag.name} ({tag.link_count})
        </option>
      ))}
    </select>
  );
}
