import { useState } from 'react';
import { setApiKey } from '../api/client';

interface ApiKeyPromptProps {
  onKeySet: () => void;
}

export function ApiKeyPrompt({ onKeySet }: ApiKeyPromptProps) {
  const [key, setKey] = useState('');
  const [error, setError] = useState('');

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = key.trim();
    if (!trimmed) {
      setError('API key is required');
      return;
    }
    setApiKey(trimmed);
    setError('');
    onKeySet();
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg shadow-xl p-6 w-full max-w-md">
        <h2 className="text-lg font-semibold mb-4">Enter API Key</h2>
        <p className="text-sm text-gray-600 mb-4">
          An API key is required to access the dashboard.
        </p>
        <form onSubmit={handleSubmit}>
          <input
            type="password"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder="API key"
            className="w-full border rounded px-3 py-2 mb-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
            autoFocus
          />
          {error && (
            <p className="text-sm text-red-600 mb-2">{error}</p>
          )}
          <button
            type="submit"
            className="w-full bg-blue-600 text-white rounded px-4 py-2 hover:bg-blue-700"
          >
            Connect
          </button>
        </form>
      </div>
    </div>
  );
}
