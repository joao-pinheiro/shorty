import { useState, useEffect } from 'react';
import { format, parseISO } from 'date-fns';
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts';
import { api, AuthError, ApiRequestError } from '../api/client';
import type { Analytics } from '../types';

interface AnalyticsPanelProps {
  linkId: number;
  onAuthError: () => void;
}

const PERIODS = ['24h', '7d', '30d', 'all'] as const;

export function AnalyticsPanel({ linkId, onAuthError }: AnalyticsPanelProps) {
  const [period, setPeriod] = useState<'24h' | '7d' | '30d' | 'all'>('7d');
  const [analytics, setAnalytics] = useState<Analytics | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);

    api.getAnalytics(linkId, period)
      .then((data) => {
        if (!cancelled) setAnalytics(data);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof AuthError) { onAuthError(); return; }
        setError(err instanceof ApiRequestError ? err.message : 'Failed to load analytics');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => { cancelled = true; };
  }, [linkId, period, onAuthError]);

  const chartData = analytics
    ? period === '24h'
      ? (analytics.clicks_by_hour ?? []).map(h => ({
          label: format(parseISO(h.hour), 'HH:mm'),
          count: h.count,
        }))
      : (analytics.clicks_by_day ?? []).map(d => ({
          label: format(parseISO(d.date + 'T00:00:00'), 'MMM d'),
          count: d.count,
        }))
    : [];

  return (
    <div className="p-4">
      <div className="flex items-center justify-between mb-3">
        <div className="flex gap-1">
          {PERIODS.map((p) => (
            <button
              key={p}
              onClick={() => setPeriod(p)}
              className={`px-3 py-1 text-xs rounded ${
                period === p
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-200 text-gray-700 hover:bg-gray-300'
              }`}
            >
              {p}
            </button>
          ))}
        </div>
        {analytics && (
          <span className="text-sm text-gray-600">
            Total: {analytics.total_clicks.toLocaleString()} | Period: {analytics.period_clicks.toLocaleString()}
          </span>
        )}
      </div>

      {loading && <div className="text-sm text-gray-500 py-4">Loading analytics...</div>}
      {error && <div className="text-sm text-red-600 py-4">{error}</div>}

      {!loading && !error && chartData.length > 0 && (
        <ResponsiveContainer width="100%" height={200}>
          <BarChart data={chartData}>
            <XAxis dataKey="label" fontSize={12} />
            <YAxis allowDecimals={false} fontSize={12} />
            <Tooltip />
            <Bar dataKey="count" fill="#3b82f6" radius={[2, 2, 0, 0]} />
          </BarChart>
        </ResponsiveContainer>
      )}

      {!loading && !error && chartData.length === 0 && (
        <div className="text-sm text-gray-500 py-4">No click data for this period.</div>
      )}
    </div>
  );
}
