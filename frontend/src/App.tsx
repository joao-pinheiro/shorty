import { useState, useCallback, useEffect } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Dashboard } from './pages/Dashboard';
import { NotFound } from './pages/NotFound';
import { ApiKeyPrompt } from './components/ApiKeyPrompt';
import { hasApiKey, clearApiKey, api, AuthError } from './api/client';

export default function App() {
  const [needsKey, setNeedsKey] = useState(!hasApiKey());

  const handleKeySet = useCallback(() => {
    setNeedsKey(false);
  }, []);

  const handleAuthError = useCallback(() => {
    clearApiKey();
    setNeedsKey(true);
  }, []);

  useEffect(() => {
    if (!needsKey) {
      api.getLinks({ page: 1, per_page: 1 }).catch((err) => {
        if (err instanceof AuthError) {
          handleAuthError();
        }
      });
    }
  }, [needsKey, handleAuthError]);

  return (
    <BrowserRouter>
      {needsKey && <ApiKeyPrompt onKeySet={handleKeySet} />}
      <div className="min-h-screen bg-gray-50">
        <header className="bg-white shadow-sm border-b">
          <div className="max-w-7xl mx-auto px-4 py-4 flex items-center justify-between">
            <h1 className="text-xl font-bold text-gray-900">Shorty</h1>
            {!needsKey && (
              <button
                onClick={handleAuthError}
                className="text-sm text-gray-500 hover:text-gray-700"
              >
                Change API Key
              </button>
            )}
          </div>
        </header>
        <main className="max-w-7xl mx-auto px-4 py-6">
          <Routes>
            <Route path="/" element={<Dashboard onAuthError={handleAuthError} />} />
            <Route path="*" element={<NotFound />} />
          </Routes>
        </main>
      </div>
    </BrowserRouter>
  );
}
