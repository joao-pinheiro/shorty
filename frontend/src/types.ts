export interface Link {
  id: number;
  code: string;
  short_url: string;
  original_url: string;
  created_at: string;
  expires_at: string | null;
  is_active: boolean;
  click_count: number;
  updated_at: string;
  tags: string[];
}

export interface PaginatedLinks {
  links: Link[];
  total: number;
  page: number;
  per_page: number;
}

export interface ListParams {
  page?: number;
  per_page?: number;
  search?: string;
  sort?: 'created_at' | 'click_count' | 'expires_at';
  order?: 'asc' | 'desc';
  active?: boolean;
  tag?: string;
}

export interface LinkPatch {
  is_active?: boolean;
  expires_at?: string | null;
  tags?: string[];
}

export interface BulkRequestItem {
  url: string;
  custom_code?: string;
  expires_in?: number;
  tags?: string[];
}

export interface BulkResultItem {
  ok: boolean;
  link?: Link;
  error?: string;
  index?: number;
}

export interface BulkResponse {
  total: number;
  succeeded: number;
  failed: number;
  results: BulkResultItem[];
}

export interface DayCount {
  date: string;
  count: number;
}

export interface HourCount {
  hour: string;
  count: number;
}

export interface Analytics {
  link_id: number;
  total_clicks: number;
  period_clicks: number;
  clicks_by_day?: DayCount[];
  clicks_by_hour?: HourCount[];
}

export interface Tag {
  id: number;
  name: string;
  created_at: string;
}

export interface TagWithCount extends Tag {
  link_count: number;
}

export interface ApiError {
  error: string;
  retry_after?: number;
}
