# Phase 11: Frontend Features

## Summary

Implements the remaining frontend components: analytics panel with Recharts bar chart, bulk shorten modal, QR code modal, tag filtering, and tag management UI. All components integrate into the existing Dashboard page from Phase 10. References: S15.2, S6.3, S6.8, S6.9, S6.10, S6.11, S6.12.

**Depends on**: Phase 10 (core dashboard with ShortenForm, LinkTable, SearchBar, useLinks hook, API client).

---

## Files to Create/Modify

| File | Action |
|------|--------|
| `frontend/src/components/AnalyticsPanel.tsx` | Create |
| `frontend/src/components/BulkShortenModal.tsx` | Create |
| `frontend/src/components/QRCodeModal.tsx` | Create |
| `frontend/src/components/TagFilter.tsx` | Create |
| `frontend/src/components/TagManager.tsx` | Create |
| `frontend/src/hooks/useTags.ts` | Create |
| `frontend/src/pages/Dashboard.tsx` | Modify — wire in new components |
| `frontend/src/components/LinkRow.tsx` | Modify — add analytics expand, QR button handlers |

---

## Step 1: `useTags` Hook

**File**: `frontend/src/hooks/useTags.ts`

This hook manages tag list state and provides create/delete operations. Used by both `TagFilter` and `TagManager`.

### Interface

```typescript
import { useState, useEffect, useCallback } from 'react';
import { TagWithCount } from '../types';
import { api } from '../api/client';

interface UseTagsReturn {
  tags: TagWithCount[];
  loading: boolean;
  error: string | null;
  createTag: (name: string) => Promise<TagWithCount>;
  deleteTag: (id: number) => Promise<void>;
  refetch: () => Promise<void>;
}

export function useTags(): UseTagsReturn;
```

### State

```typescript
const [tags, setTags] = useState<TagWithCount[]>([]);
const [loading, setLoading] = useState(true);
const [error, setError] = useState<string | null>(null);
```

### Behavior

- On mount, call `api.getTags()` and populate `tags` state.
- `createTag(name)`: calls `api.createTag(name)`, on success adds the returned tag to local state and returns it. On error, sets `error` state and rethrows.
- `deleteTag(id)`: calls `api.deleteTag(id)`, on success removes from local state. On error, sets `error` state and rethrows.
- `refetch()`: re-fetches full tag list from API. Called after operations that may change link counts (e.g., after link creation/deletion).

### API Methods Used (from S15.4)

```typescript
// Already defined in api/client.ts from Phase 9:
getTags(): Promise<Tag[]>
createTag(name: string): Promise<Tag>
deleteTag(id: number): Promise<void>
```

### Types Required (from S6.10, S6.11 — defined in types.ts from Phase 9)

```typescript
interface Tag {
  id: number;
  name: string;
  created_at: string;
  link_count: number;  // present in list response (S6.10)
}
```

---

## Step 2: AnalyticsPanel Component

**File**: `frontend/src/components/AnalyticsPanel.tsx`

Inline expandable panel shown beneath a link row. Displays total click count and a Recharts bar chart of clicks over time. Period selector switches between 24h, 7d, 30d, and all.

### Props

```typescript
interface AnalyticsPanelProps {
  linkId: number;
  onClose: () => void;
}
```

### State

```typescript
const [period, setPeriod] = useState<'24h' | '7d' | '30d' | 'all'>('7d');
const [analytics, setAnalytics] = useState<Analytics | null>(null);
const [loading, setLoading] = useState(true);
const [error, setError] = useState<string | null>(null);
```

### Types (from S6.8)

```typescript
// In types.ts:
interface Analytics {
  link_id: number;
  total_clicks: number;
  period_clicks: number;
  clicks_by_day?: { date: string; count: number }[];
  clicks_by_hour?: { hour: string; count: number }[];
}
```

### Behavior

1. On mount and whenever `period` changes, fetch analytics: `api.getAnalytics(linkId, period)`.
2. While loading, show a spinner or skeleton.
3. On error, display error message inline.
4. Render:
   - **Period selector**: four buttons (`24h`, `7d`, `30d`, `all`), active state highlighted with Tailwind classes (e.g., `bg-blue-600 text-white` for active, `bg-gray-200` for inactive).
   - **Summary**: "Total clicks: {total_clicks} | Period clicks: {period_clicks}"
   - **Bar chart** using Recharts:
     - For `24h` period: use `clicks_by_hour` array, X-axis = hour (formatted with `date-fns` as `HH:mm`), Y-axis = count.
     - For `7d`, `30d`, `all` periods: use `clicks_by_day` array, X-axis = date (formatted as `MMM dd`), Y-axis = count.
   - **Close button** in top-right corner, calls `onClose`.

### Recharts Usage

```tsx
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts';

// Chart data derived from analytics response:
const chartData = period === '24h'
  ? (analytics.clicks_by_hour ?? []).map(h => ({
      label: format(parseISO(h.hour), 'HH:mm'),
      count: h.count,
    }))
  : (analytics.clicks_by_day ?? []).map(d => ({
      label: format(parseISO(d.date), 'MMM dd'),
      count: d.count,
    }));

<ResponsiveContainer width="100%" height={200}>
  <BarChart data={chartData}>
    <XAxis dataKey="label" fontSize={12} />
    <YAxis allowDecimals={false} fontSize={12} />
    <Tooltip />
    <Bar dataKey="count" fill="#3b82f6" radius={[2, 2, 0, 0]} />
  </BarChart>
</ResponsiveContainer>
```

### Layout

- Full-width row spanning all table columns (rendered as a `<tr>` with a single `<td colSpan={...}>`).
- Light gray background (`bg-gray-50`), padding `p-4`, border top `border-t`.
- Max height reasonable for inline display (chart at 200px height).

---

## Step 3: BulkShortenModal Component

**File**: `frontend/src/components/BulkShortenModal.tsx`

Modal dialog for bulk URL shortening. One URL per line in a textarea. Shows per-line results after submission.

### Props

```typescript
interface BulkShortenModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccess: () => void;  // called to trigger link list refetch
}
```

### State

```typescript
const [input, setInput] = useState('');
const [submitting, setSubmitting] = useState(false);
const [results, setResults] = useState<BulkResult[] | null>(null);
const [summary, setSummary] = useState<{ total: number; succeeded: number; failed: number } | null>(null);
```

### Types (from S6.3)

```typescript
// In types.ts:
interface BulkRequest {
  url: string;
  custom_code?: string;
  expires_in?: number;
  tags?: string[];
}

interface BulkResultItem {
  ok: boolean;
  link?: Link;
  error?: string;
  index?: number;
}

interface BulkResponse {
  total: number;
  succeeded: number;
  failed: number;
  results: BulkResultItem[];
}
```

### Behavior

1. **Input phase**:
   - Textarea with placeholder: "Enter one URL per line (max 50)".
   - Line count indicator below textarea: "{n} URLs" (count non-empty lines).
   - If more than 50 non-empty lines, show warning and disable submit.
   - "Shorten All" button (disabled while submitting or no valid input).
   - "Cancel" button calls `onClose`.

2. **Submission**:
   - Parse textarea: split by newline, trim each line, filter empty lines.
   - Build request: `{ urls: lines.map(url => ({ url })) }`.
   - Call `api.createBulkLinks(urls)`.
   - Store response in `results` and `summary` state.

3. **Results phase**:
   - Show summary: "Created {succeeded} of {total} links ({failed} failed)".
   - Per-line results list:
     - Success: green checkmark icon + short URL + copy button.
     - Failure: red X icon + original URL + error message.
   - "Close" button calls `onClose` then `onSuccess` (to refetch link list).

### Layout

- Modal overlay: fixed inset-0, bg-black/50, flex center.
- Modal content: bg-white, rounded-lg, max-w-lg, w-full, max-h-[80vh] overflow-y-auto.
- Header: "Bulk Shorten URLs", close X button.
- Body: textarea (rows=10, w-full, font-mono, border, p-2) or results list.
- Footer: action buttons.

---

## Step 4: QRCodeModal Component

**File**: `frontend/src/components/QRCodeModal.tsx`

Modal displaying a QR code image for a link's short URL with download capability.

### Props

```typescript
interface QRCodeModalProps {
  isOpen: boolean;
  onClose: () => void;
  linkId: number;
  shortUrl: string;  // displayed as label
  code: string;      // used to construct public QR URL
}
```

### State

```typescript
const [loading, setLoading] = useState(true);
const [error, setError] = useState<string | null>(null);
```

### Behavior

1. The QR code image is fetched from the API endpoint: `GET /api/v1/links/:id/qr?size=256` (authenticated).
   - Use `api.getQRCodeBlob(linkId, 256)` which returns a `Promise<Blob>` with auth handled internally (from S15.4).

2. **Fetch as blob and create object URL**:
   ```typescript
   const [qrUrl, setQrUrl] = useState<string | null>(null);

   useEffect(() => {
     if (!isOpen) return;
     setLoading(true);
     setError(null);

     api.getQRCodeBlob(linkId, 256)
       .then(blob => {
         setQrUrl(URL.createObjectURL(blob));
       })
       .catch(err => {
         setError(err instanceof Error ? err.message : 'Failed to load QR code');
       })
       .finally(() => {
         setLoading(false);
       });

     return () => {
       if (qrUrl) URL.revokeObjectURL(qrUrl);
     };
   }, [isOpen, linkId]);
   ```

3. **Download button**: Create an anchor element programmatically using the same object URL.
   ```typescript
   const handleDownload = () => {
     if (!qrUrl) return;
     const a = document.createElement('a');
     a.href = qrUrl;
     a.download = `qr-${code}.png`;
     a.click();
   };
   ```

4. Display:
   - QR code image centered, 256x256.
   - Short URL label below the image (non-editable text).
   - "Download PNG" button.
   - "Close" button.

### Layout

- Same modal overlay pattern as BulkShortenModal.
- Modal content: bg-white, rounded-lg, max-w-sm, centered.
- Image: `mx-auto` with border, rounded.

---

## Step 5: TagFilter Component

**File**: `frontend/src/components/TagFilter.tsx`

Dropdown to filter the link list by a single tag. Integrates into the SearchBar area above the link table.

### Props

```typescript
interface TagFilterProps {
  tags: Tag[];
  selectedTag: string | null;
  onChange: (tag: string | null) => void;
}
```

### Behavior

1. Render a `<select>` dropdown (or custom dropdown with Tailwind).
2. Options:
   - "All tags" (value: empty/null) — default.
   - One option per tag: `{tag.name} ({tag.link_count})`.
3. On change, call `onChange` with the selected tag name or `null` for "All tags".
4. The parent component (`Dashboard`) passes the selected tag into `useLinks` hook params, which includes it as the `tag` query parameter on `GET /api/v1/links` (S6.4).

### Layout

- Inline with other search/filter controls.
- `<select>` styled with Tailwind: `border rounded px-3 py-2 text-sm`.

---

## Step 6: TagManager Component

**File**: `frontend/src/components/TagManager.tsx`

A panel (or modal) for managing tags: listing all tags with link counts, creating new tags, and deleting tags.

### Props

```typescript
interface TagManagerProps {
  isOpen: boolean;
  onClose: () => void;
}
```

### State (internal, uses useTags hook)

```typescript
const { tags, loading, error, createTag, deleteTag, refetch } = useTags();
const [newTagName, setNewTagName] = useState('');
const [creating, setCreating] = useState(false);
const [createError, setCreateError] = useState<string | null>(null);
```

### Behavior

1. **Tag list**:
   - Table or list showing each tag: name, link count, created date, delete button.
   - Delete button: confirm dialog ("Delete tag '{name}'? It will be removed from all links."), then call `deleteTag(id)`.
   - If no tags exist, show "No tags yet" message.

2. **Create tag form**:
   - Text input for tag name with inline validation.
   - Validation: must match `^[a-zA-Z0-9_-]{1,50}$` (S6.11). Show error message inline if invalid.
   - "Add Tag" button, disabled while `creating` or input is empty/invalid.
   - On submit: call `createTag(name)`, clear input on success.
   - Handle 409 (duplicate) and 400 (limit reached / invalid name) errors from API, display inline.

3. **Error display**: show `error` from hook and `createError` as dismissible alert.

### Layout

- Modal overlay (same pattern as other modals).
- Modal content: bg-white, rounded-lg, max-w-md.
- Header: "Manage Tags" + close button.
- Create form at top: input + button in a flex row.
- Tag list below: scrollable if many tags (`max-h-[60vh] overflow-y-auto`).
- Each tag row: flex row with name (bold), link count (gray), created date (gray, smaller), delete icon button (red).

### Tag Name Validation (client-side)

```typescript
const TAG_REGEX = /^[a-zA-Z0-9_-]{1,50}$/;

const isValidTagName = (name: string): boolean => TAG_REGEX.test(name);
```

---

## Step 7: Update LinkRow for Analytics and QR

**File**: `frontend/src/components/LinkRow.tsx` (modify)

### Changes

Add state and callbacks for expanding analytics and opening QR modal:

```typescript
// Additional props needed in LinkRow:
interface LinkRowProps {
  link: Link;
  onDelete: (id: number) => void;
  onToggleActive: (id: number, isActive: boolean) => void;
  onShowAnalytics: (id: number) => void;    // NEW
  onShowQR: (id: number, code: string, shortUrl: string) => void;  // NEW
  isAnalyticsOpen: boolean;                  // NEW
}
```

### Action Buttons (per row)

Each row renders these action buttons in the Actions column:

1. **Copy** — existing CopyButton from Phase 10.
2. **QR Code** — button with QR icon, calls `onShowQR(link.id, link.code, link.short_url)`.
3. **Analytics** — button with chart icon, calls `onShowAnalytics(link.id)`. Highlighted when `isAnalyticsOpen`.
4. **Activate/Deactivate** — toggle button, calls `onToggleActive(link.id, !link.is_active)`.
5. **Delete** — button with trash icon, calls `onDelete(link.id)` after confirmation.

Button styling: icon-only buttons in a flex row with gap-1, `text-gray-500 hover:text-gray-700`, active analytics button `text-blue-600`.

---

## Step 8: Wire into Dashboard

**File**: `frontend/src/pages/Dashboard.tsx` (modify)

### New State in Dashboard

```typescript
// Tag management
const { tags, refetch: refetchTags } = useTags();
const [selectedTag, setSelectedTag] = useState<string | null>(null);

// Analytics expand
const [analyticsLinkId, setAnalyticsLinkId] = useState<number | null>(null);

// Modals
const [bulkModalOpen, setBulkModalOpen] = useState(false);
const [qrModal, setQrModal] = useState<{ linkId: number; code: string; shortUrl: string } | null>(null);
const [tagManagerOpen, setTagManagerOpen] = useState(false);
```

### Integration Points

1. **Pass `selectedTag` into `useLinks` params** so the link list filters by tag when one is selected.

2. **SearchBar area additions**:
   - Add `<TagFilter>` next to existing search/sort/filter controls.
   - Add "Bulk Shorten" button that opens BulkShortenModal.
   - Add "Manage Tags" button that opens TagManager.

3. **LinkTable rendering**:
   - For each `LinkRow`, pass `onShowAnalytics` and `onShowQR` handlers.
   - Track `analyticsLinkId` — when a row's analytics is toggled:
     - If same link, close (set to null).
     - If different link, switch to new link.
   - After each `LinkRow` where `analyticsLinkId === link.id`, render `<AnalyticsPanel>` in a full-width table row.

4. **Modal rendering** (at bottom of Dashboard return):
   ```tsx
   <BulkShortenModal
     isOpen={bulkModalOpen}
     onClose={() => setBulkModalOpen(false)}
     onSuccess={() => { refetchLinks(); refetchTags(); }}
   />
   {qrModal && (
     <QRCodeModal
       isOpen={true}
       onClose={() => setQrModal(null)}
       linkId={qrModal.linkId}
       shortUrl={qrModal.shortUrl}
       code={qrModal.code}
     />
   )}
   <TagManager
     isOpen={tagManagerOpen}
     onClose={() => { setTagManagerOpen(false); refetchTags(); }}
   />
   ```

5. **After link creation** in ShortenForm's onSuccess, also call `refetchTags()` (creating a link with new tags changes tag counts).

### Updated Dashboard Layout (top to bottom)

```
┌──────────────────────────────────────────────┐
│  ShortenForm (URL input, options, submit)     │
│  [Result display after creation]              │
├──────────────────────────────────────────────┤
│  SearchBar | TagFilter | [Bulk Shorten] [Tags]│
├──────────────────────────────────────────────┤
│  LinkTable                                    │
│    LinkRow (link 1)                           │
│    [AnalyticsPanel if expanded]               │
│    LinkRow (link 2)                           │
│    ...                                        │
│  Pagination                                   │
└──────────────────────────────────────────────┘
[BulkShortenModal]  [QRCodeModal]  [TagManager]
```

---

## Step 9: ShortenForm Tag Integration

**File**: `frontend/src/components/ShortenForm.tsx` (modify — should already exist from Phase 10)

### Changes

The ShortenForm from Phase 10 included an optional tag multi-select. Ensure it's properly wired:

1. Accept `tags: Tag[]` as a prop (from `useTags` in Dashboard).
2. Render a multi-select or tag input:
   - Show existing tags as selectable chips/checkboxes.
   - Allow typing a new tag name (auto-creates on submit via the API's tag auto-creation in S6.2).
3. State:
   ```typescript
   const [selectedTags, setSelectedTags] = useState<string[]>([]);
   ```
4. On submit, pass `selectedTags` to `api.createLink(url, customCode, expiresIn, selectedTags)`.

### Tag Input UX

- Render existing tags as clickable pills below the input.
- Clicking toggles selection (selected = filled blue, unselected = outline gray).
- Text input allows typing a new tag name. On Enter or comma, add to selected list.
- Validate new tag names client-side: `^[a-zA-Z0-9_-]{1,50}$`.

---

## Verification Checklist

1. **Analytics Panel**:
   - Click analytics button on a link row — panel expands below.
   - Click again — panel collapses.
   - Switch period buttons — chart re-fetches and updates.
   - 24h shows hourly bars; 7d/30d/all show daily bars.
   - Total and period click counts display correctly.

2. **Bulk Shorten Modal**:
   - Click "Bulk Shorten" button — modal opens.
   - Enter 3 URLs (one invalid) — submit.
   - Results show 2 successes (with short URLs + copy buttons) and 1 failure (with error message).
   - Close modal — link table refreshes with new links.
   - Enter > 50 URLs — submit button disabled with warning.

3. **QR Code Modal**:
   - Click QR button on a link row — modal opens with QR image.
   - Click "Download PNG" — browser downloads the image.
   - Close modal.

4. **Tag Filter**:
   - Select a tag from dropdown — link table filters to only links with that tag.
   - Select "All tags" — filter removed.

5. **Tag Manager**:
   - Click "Manage Tags" — modal opens.
   - Create a new tag — appears in list.
   - Try creating duplicate — error shown.
   - Try creating tag with invalid characters — validation error.
   - Delete a tag — confirm dialog — tag removed from list.
   - Close modal — tag filter dropdown reflects changes.
